import boto3
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
        
        # Terminate the instances directly since we're cleaning up
        instance_ids = []
        for reservation in response['Reservations']:
            for instance in reservation['Instances']:
                instance_ids.append(instance['InstanceId'])
        
        if instance_ids:
            logger.info(f"Terminating instances: {instance_ids}")
            try:
                ec2.terminate_instances(InstanceIds=instance_ids)
            except Exception as e:
                logger.warning(f"Error terminating instances: {str(e)}")
        
        time.sleep(15)
    
    logger.warning(f"Timeout waiting for Karpenter instances to terminate")
    return False


def cleanup_instance_profiles(cluster_name, region, max_retries=5):
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
                                time.sleep(30)
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
    
    This Lambda cleans up:
    1. Terminates any running Karpenter-managed EC2 instances
    2. Waits for instances to terminate
    3. Removes instance profiles from the KarpenterNodeRole
    4. Deletes the instance profiles
    
    This allows CloudFormation to delete the KarpenterNodeRole IAM role.
    """
    request_type = event['RequestType']
    cluster_name = event['ResourceProperties']['ClusterName']
    # Get region from ResourceProperties (passed from CDK) or fallback to environment variable
    region = event['ResourceProperties'].get('Region') or os.environ.get('AWS_REGION')
    if not region:
        raise ValueError('Region must be provided via ResourceProperties or AWS_REGION environment variable')
    
    logger.info(f"Request type: {request_type}, Cluster: {cluster_name}, Region: {region}")
    
    # Only run cleanup on delete
    if request_type != 'Delete':
        return send_response(event, context, 'SUCCESS', {'Message': 'No cleanup needed'})
    
    try:
        # Step 1: Terminate any running Karpenter-managed EC2 instances and wait
        logger.info("Step 1: Terminating and waiting for Karpenter-managed EC2 instances...")
        wait_for_karpenter_nodes_terminated(cluster_name, region, timeout=300)
        
        # Step 2: Clean up instance profiles
        logger.info("Step 2: Cleaning up Karpenter instance profiles...")
        cleanup_instance_profiles(cluster_name, region, max_retries=5)
        
        logger.info("Cleanup completed successfully")
        return send_response(event, context, 'SUCCESS', {'Message': 'Karpenter resources cleaned up successfully'})
        
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
