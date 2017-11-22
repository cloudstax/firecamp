The cloudformation template to install Redis directly. It includes the lambda function to create the Redis service.

Usage:
- create a s3 bucket such as "qscloudstax"
- create top folder "cloudstax"
- create folder "cloudstax/firecamp/latest", upload the templates, scripts and packages of the last release such as 0.9.
- create folder "cloudstax/redis/latest", upload the 4 local templates to "templates" subfolder, and upload $GOAPTH/bin/redis-lambda.zip to "scripts" subfolder.
then could deploy the redis cluster via cloudformation.
