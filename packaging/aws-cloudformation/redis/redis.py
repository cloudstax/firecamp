import json
import sys
import requests
import time
import socket

import boto3

# example event
# event: {'RequestType': 'Create', 'ServiceToken': 'arn:aws:lambda:us-east-1:497621646529:function:lam-LambdaFunction-1T5JR77VYGBTG', 'ResponseURL': 'https://cloudformation-custom-resource-response-useast1.s3.amazonaws.com/arn%3Aaws%3Acloudformation%3Aus-east-1%3A497621646529%3Astack/lam/98f74630-c1d0-11e7-84b6-50faeaa96461%7CLambdaCustomResource%7C0b637bff-c8f6-4ace-bb6f-1a6e92f29c8a?AWSAccessKeyId=AKIAJNXHFR7P7YGKLDPQ&Expires=1509855955&Signature=fuzleqrW%2BXFf8ncDFJiEhEVn9d4%3D', 'StackId': 'arn:aws:cloudformation:us-east-1:497621646529:stack/lam/98f74630-c1d0-11e7-84b6-50faeaa96461', 'RequestId': '0b637bff-c8f6-4ace-bb6f-1a6e92f29c8a', 'LogicalResourceId': 'LambdaCustomResource', 'ResourceType': 'Custom::RedisLambdaCustomResource', 'ResourceProperties': {'ServiceToken': 'arn:aws:lambda:us-east-1:497621646529:function:lam-LambdaFunction-1T5JR77VYGBTG', 'ReplTimeoutSecs': '60', 'DisableAOF': 'false', 'VolumeSizeGB': '1', 'ConfigCmdName': '', 'Cluster': 't1', 'ReplicasPerShard': '1', 'MaxMemPolicy': 'noeviction', 'AuthPass': '', 'MemoryCacheSizeMB': '256', 'ServiceName': 'myredis', 'Region': 'us-east-1', 'Shards': '1'}}

def lambda_handler(event, context):
    reason = 'unknown exception'
    # https://aws.amazon.com/premiumsupport/knowledge-center/best-practices-custom-cf-lambda/
    try:
        print(time.strftime('%Y-%m-%d %H:%M:%S'), event)

        requestType = event['RequestType']
        properties = event['ResourceProperties']

        manageserver='firecamp-manageserver.'+properties['Cluster']+'-firecamp.com'
        # wait till dns is updated for firecamp manageserver
        for i in range(30):
            try:
                print(manageserver, socket.gethostbyname(manageserver))
                break
            except:
                print("lookup manageserver ip error:", sys.exc_info(), manageserver)
                time.sleep(3)

        print(time.strftime('%Y-%m-%d %H:%M:%S'), manageserver, socket.gethostbyname(manageserver))

        if requestType == 'Create':
            shards = int(properties['Shards'])
            disableAOF = False
            if properties['DisableAOF'] == 'true':
                disableAOF = True

            data = {
                "Service": {
                    "Region": properties['Region'],
                    "Cluster": properties['Cluster'],
                    "ServiceName": properties['ServiceName']
                },
                "Resource": {
                    "MaxCPUUnits": 0,
                    "ReserveCPUUnits": 256,
                    "MaxMemMB": 0,
                    "ReserveMemMB": 256
                },
                "Options": {
                    "Shards": shards,
                    "ReplicasPerShard": int(properties['ReplicasPerShard']),
                    "MemoryCacheSizeMB": int(properties['MemoryCacheSizeMB']),
                    "Volume": {
                        "VolumeType": properties['VolumeType'],
                        "Iops": int(properties['Iops']),
                        "VolumeSizeGB": int(properties['VolumeSizeGB']),
                    },
                    "DisableAOF": disableAOF,
                    "AuthPass": properties['AuthPass'],
                    "ReplTimeoutSecs": int(properties['ReplTimeoutSecs']),
                    "MaxMemPolicy": properties['MaxMemPolicy'],
                    "ConfigCmdName": properties['ConfigCmdName']
                }
            }

            print(data)

            url = 'http://' + manageserver + ':27040/?Catalog-Create-Redis'
            headers = {'Content-type': 'application/json'}

            # retry 3 times
            reason = 'create redis failed'
            succ = False
            for i in range(3):
                try:
                    rsp = requests.put(url, data=json.dumps(data), headers=headers, timeout=60)
                    if rsp.status_code == 200:
                        print(time.strftime('%Y-%m-%d %H:%M:%S'), "redis service created")
                        succ = True
                        break

                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "create redis failed, status_code:", rsp.status_code, "reason:", rsp.reason)
                    reason = rsp.reason
                    time.sleep(5)
                except:
                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "create redis error:", sys.exc_info())
                    time.sleep(5)

            if succ != True:
                sendResponse(event, context, 'FAILED', reason, '')
                return

            if shards < 3:
                sendResponse(event, context, 'SUCCESS', '', '')
                return

            # wait till redis cluster initialized

            url = 'http://' + manageserver + ':27040/?Catalog-Check-Service-Init'
            initdata = {
                "ServiceType": "redis",
                "Service": {
                    "Region": properties['Region'],
                    "Cluster": properties['Cluster'],
                    "ServiceName": properties['ServiceName']
                },
            }

            print(time.strftime('%Y-%m-%d %H:%M:%S'), "wait redis service initialized", initdata)

            reason = 'wait redis init timed out'
            for i in range(40):
                try:
                    rsp = requests.get(url, data=json.dumps(initdata), headers=headers, timeout=20)
                    if rsp.status_code == 200:
                        initres = json.loads(rsp.content)
                        print(time.strftime('%Y-%m-%d %H:%M:%S'), "redis init status:", initres)
                        if initres["Initialized"]:
                            sendResponse(event, context, 'SUCCESS', '', '')
                            return
                    else:
                        print(time.strftime('%Y-%m-%d %H:%M:%S'), "wait redis init failed, status_code:", rsp.status_code, "reason:", rsp.reason)
                        reason = rsp.reason

                    time.sleep(5)
                except:
                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "wait redis error:", sys.exc_info())
                    time.sleep(5)

            sendResponse(event, context, 'FAILED', reason, '')
            return

        elif requestType == 'Delete':
            data = {
                "Service": {
                    "Region": properties['Region'],
                    "Cluster": properties['Cluster'],
                    "ServiceName": properties['ServiceName']
                }
            }

            url = 'http://' + manageserver + ':27040/?Delete-Service'
            headers = {'Content-type': 'application/json'}

            # retry 3 times
            reason = 'delete redis time out'
            for i in range(3):
                try:
                    rsp = requests.delete(url, data=json.dumps(data), headers=headers, timeout=160)
                    if rsp.status_code == 200:
                        respdata = json.loads(rsp.content)
                        print(time.strftime('%Y-%m-%d %H:%M:%S'), "redis service deleted, please manually delete the volumes", respdata)

                        if properties['DeleteVolume'] == 'true':
                            print("DeleteVolume is set to true")
                            ec2 = boto3.resource('ec2')
                            for volid in respdata["VolumeIDs"]:
                                volume = ec2.Volume(volid)
                                try:
                                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "volume", volid, "state", volume.state)
                                    for waitVolume in range(5):
                                        if volume.state == "available":
                                            break
                                        else:
                                            time.sleep(5)
                                            volume.load()

                                    volume.delete()
                                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "deleted volume", volid, "state", volume.state)
                                except:
                                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "delete volume", volid, "state", volume.state, "error:", sys.exc_info())

                        sendResponse(event, context, 'SUCCESS', '', respdata)
                        return
                    else:
                        print(time.strftime('%Y-%m-%d %H:%M:%S'), "delete redis failed, status_code:", rsp.status_code, "reason:", rsp.reason)
                        reason = rsp.reason
                        time.sleep(5)
                except:
                    print(time.strftime('%Y-%m-%d %H:%M:%S'), "delete redis error:", sys.exc_info())
                    time.sleep(5)

            sendResponse(event, context, 'FAILED', reason, '')
            return

        elif requestType == 'Update':
            sendResponse(event, context, 'SUCCESS', '', '')
            return

        else:
            sendResponse(event, context, 'FAILED', reason, '')
            return
    except:
        print(time.strftime('%Y-%m-%d %H:%M:%S'), 'error:', sys.exc_info(), 'event:', event)
        sendResponse(event, context, 'FAILED', reason, '')
        return

# http://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/crpg-ref-responses.html
def sendResponse(event, context, status, reason, responseData):
    try:
        body = {
            'Status': status,
            'Reason': reason,
            'PhysicalResourceId': 'redis-' + event['LogicalResourceId'],
            'StackId': event['StackId'],
            'RequestId': event['RequestId'],
            'LogicalResourceId': event['LogicalResourceId']
        }
        if responseData != '':
            body['Data'] = responseData

        print(time.strftime('%Y-%m-%d %H:%M:%S'), 'response body:', body)

        url = event['ResponseURL']
        print(url)

        bodydata = json.dumps(body)
        headers = { 'Content-type': '', 'content-length': str(len(bodydata)) }

        rsp = requests.put(url, data=bodydata, headers=headers)

        print(time.strftime('%Y-%m-%d %H:%M:%S'), 'send response result:', rsp.status_code, 'reason', rsp.reason)
    except:
        print(time.strftime('%Y-%m-%d %H:%M:%S'), 'sendResponse error:', sys.exc_info())

