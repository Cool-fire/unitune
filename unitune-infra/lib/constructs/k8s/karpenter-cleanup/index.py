import boto3
import subprocess
import json
import os
import logging
import time

logger = logging.getLogger()
logger.setLevel(logging.INFO)


def wait_for_karpenter_nodes_terminated(cluster_name, region, timeout=300):
    """
    Wait for all Karpenter-managed EC2 instances to terminate.
    Karpenter tags instances with karpenter.sh/nodepool.
    """
    ec2 = boto3.client('ec2', region_name=region)
    start_time = time.time()
    
    while time.time() - start_time < timeout:
        # Find running instances managed by Karpenter for this cluster
        response = ec2.describe_instances(
            Filters=[
                {'Name': 'tag:karpenter.sh/nodepool', 'Values': ['*']},
                {'Name': 'tag:karpenter.sh/discovery', 'Values': [cluster_name]},
                {'Name': 'instance-state-name', 'Values': ['pending', 'running', 'stopping', 'shutting-down']}
            ]
        )
        
        instance_count = sum(len(r['Instances']) for r in response['Reservations'])
        
        if instance_count == 0:
            logger.info("All Karpenter-managed instances have terminated")
            return True
        
        logger.info(f"Waiting for {instance_count} Karpenter instances to terminate...")
        time.sleep(15)
    
    logger.warning(f"Timeout waiting for Karpenter instances to terminate")
    return False


def cleanup_instance_profiles(cluster_name, region, max_retries=3):
    """
    Clean up instance profiles created by Karpenter.
    Karpenter creates instance profiles with names like: KarpenterNodeRole-<cluster_name>_<id>
    """
    iam = boto3.client('iam', region_name=region)
    role_name = f"KarpenterNodeRole-{cluster_name}"
    
    logger.info(f"Looking for instance profiles associated with role: {role_name}")
    
    try:
        # List all instance profiles for the Karpenter node role
        paginator = iam.get_paginator('list_instance_profiles_for_role')
        for page in paginator.paginate(RoleName=role_name):
            for profile in page['InstanceProfiles']:
                profile_name = profile['InstanceProfileName']
                logger.info(f"Found instance profile: {profile_name}")
                
                # Retry logic for instance profiles that might still be in use
                for attempt in range(max_retries):
                    try:
                        # Remove the role from the instance profile
                        logger.info(f"Removing role {role_name} from instance profile {profile_name}")
                        iam.remove_role_from_instance_profile(
                            InstanceProfileName=profile_name,
                            RoleName=role_name
                        )
                        
                        # Delete the instance profile
                        logger.info(f"Deleting instance profile: {profile_name}")
                        iam.delete_instance_profile(InstanceProfileName=profile_name)
                        logger.info(f"Successfully deleted instance profile: {profile_name}")
                        break  # Success, move to next profile
                        
                    except iam.exceptions.NoSuchEntityException:
                        logger.info(f"Instance profile {profile_name} already deleted")
                        break
                    except Exception as e:
                        error_msg = str(e)
                        if 'DeleteConflict' in error_msg or 'in use' in error_msg.lower():
                            if attempt < max_retries - 1:
                                logger.info(f"Instance profile {profile_name} still in use, waiting... (attempt {attempt + 1}/{max_retries})")
                                time.sleep(20)
                            else:
                                logger.warning(f"Failed to delete instance profile {profile_name} after {max_retries} attempts: {error_msg}")
                        else:
                            logger.warning(f"Error cleaning up instance profile {profile_name}: {error_msg}")
                            break
                    
    except iam.exceptions.NoSuchEntityException:
        logger.info(f"Role {role_name} not found, no instance profiles to clean up")
    except Exception as e:
        logger.warning(f"Error listing instance profiles: {str(e)}")


def handler(event, context):
    """
    Custom resource handler to cleanup Karpenter resources before stack deletion.
    """
    request_type = event['RequestType']
    cluster_name = event['ResourceProperties']['ClusterName']
    # Get region from ResourceProperties (passed from CDK) or fallback to environment variable
    region = event['ResourceProperties'].get('Region') or os.environ.get('AWS_REGION')
    if not region:
        raise ValueError('Region must be provided via ResourceProperties or AWS_REGION environment variable')
    
    logger.info(f"Request type: {request_type}, Cluster: {cluster_name}")
    
    # Only run cleanup on delete
    if request_type != 'Delete':
        return send_response(event, context, 'SUCCESS', {'Message': 'No cleanup needed'})
    
    try:
        # kubectl is provided by the KubectlV31Layer at /opt/kubectl/kubectl
        kubectl_path = '/opt/kubectl/kubectl'
        
        # Verify kubectl exists
        if not os.path.exists(kubectl_path):
            logger.error(f"kubectl not found at {kubectl_path}. Make sure KubectlV31Layer is attached.")
            return send_response(event, context, 'FAILED', {'Error': f'kubectl not found at {kubectl_path}'})
        
        # Verify kubectl is executable
        if not os.access(kubectl_path, os.X_OK):
            logger.error(f"kubectl at {kubectl_path} is not executable")
            return send_response(event, context, 'FAILED', {'Error': f'kubectl at {kubectl_path} is not executable'})
        
        logger.info(f"Using kubectl at: {kubectl_path}")
        
        # Verify kubectl works
        version_result = subprocess.run(
            [kubectl_path, 'version', '--client', '--short'],
            capture_output=True,
            text=True,
            timeout=10
        )
        if version_result.returncode == 0:
            logger.info(f"kubectl version: {version_result.stdout.strip()}")
        else:
            logger.warning(f"kubectl version check failed: {version_result.stderr}")
        
        # Update kubeconfig
        logger.info("Updating kubeconfig...")
        kubeconfig_result = subprocess.run(
            ['aws', 'eks', 'update-kubeconfig', '--name', cluster_name, '--region', region],
            capture_output=True,
            text=True,
            timeout=60
        )
        if kubeconfig_result.returncode != 0:
            logger.warning(f"Kubeconfig update warning: {kubeconfig_result.stderr}")
            # Continue anyway, cluster might already be partially deleted
        
        # Step 1: Delete all NodePools (this triggers node draining and termination)
        logger.info("Step 1: Deleting Karpenter NodePools...")
        nodepool_result = subprocess.run(
            [kubectl_path, 'delete', 'nodepools.karpenter.sh', '--all', '--ignore-not-found=true'],
            capture_output=True,
            text=True,
            timeout=300
        )
        if nodepool_result.returncode != 0:
            logger.warning(f"NodePool deletion warning: {nodepool_result.stderr}")
        else:
            logger.info(f"NodePool deletion output: {nodepool_result.stdout}")
        
        # Step 2: Delete all EC2NodeClasses (this triggers instance profile cleanup by Karpenter)
        logger.info("Step 2: Deleting Karpenter EC2NodeClasses...")
        nodeclass_result = subprocess.run(
            [kubectl_path, 'delete', 'ec2nodeclasses.karpenter.k8s.aws', '--all', '--ignore-not-found=true'],
            capture_output=True,
            text=True,
            timeout=300
        )
        if nodeclass_result.returncode != 0:
            logger.warning(f"EC2NodeClass deletion warning: {nodeclass_result.stderr}")
        else:
            logger.info(f"EC2NodeClass deletion output: {nodeclass_result.stdout}")
        
        # Step 3: Wait for all Karpenter-managed EC2 instances to terminate
        # This is critical - instance profiles can't be deleted while attached to running instances
        logger.info("Step 3: Waiting for Karpenter-managed EC2 instances to terminate...")
        wait_for_karpenter_nodes_terminated(cluster_name, region, timeout=300)
        
        # Step 4: Explicitly clean up instance profiles (fallback in case Karpenter didn't)
        logger.info("Step 4: Cleaning up Karpenter instance profiles...")
        cleanup_instance_profiles(cluster_name, region, max_retries=3)
        
        return send_response(event, context, 'SUCCESS', {'Message': 'Karpenter resources cleaned up successfully'})
        
    except subprocess.TimeoutExpired as e:
        logger.error(f"Command timed out: {str(e)}")
        # Return success to allow stack deletion to continue
        return send_response(event, context, 'SUCCESS', {'Message': 'Cleanup attempted but timed out'})
    except subprocess.CalledProcessError as e:
        logger.error(f"Command failed: {e.stderr}")
        # Still return success to allow stack deletion to continue
        return send_response(event, context, 'SUCCESS', {'Message': f'Cleanup attempted with warnings: {e.stderr}'})
    except Exception as e:
        logger.error(f"Unexpected error: {str(e)}")
        # Still return success to allow stack deletion to continue
        return send_response(event, context, 'SUCCESS', {'Message': f'Cleanup attempted with errors: {str(e)}'})


def send_response(event, context, status, data):
    """Send response to CloudFormation"""
    import urllib3
    
    response_body = json.dumps({
        'Status': status,
        'Reason': f'See CloudWatch Logs: {context.log_group_name}',
        'PhysicalResourceId': event.get('PhysicalResourceId', context.log_stream_name),
        'StackId': event['StackId'],
        'RequestId': event['RequestId'],
        'LogicalResourceId': event['LogicalResourceId'],
        'Data': data
    })
    
    http = urllib3.PoolManager()
    http.request(
        'PUT',
        event['ResponseURL'],
        body=response_body,
        headers={'Content-Type': 'application/json'}
    )
    
    return {'Status': status}

