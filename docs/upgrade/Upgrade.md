**AWS**

- Stop all services (*firecamp-service-cli -op=stop-service* command) but *firecamp-manageserver* and *firecamp-catalogservice*
- Upgrade AWS CloudFormation cluster, by updating it with the current template (can be found at https://aws.amazon.com/quickstart/architecture/jazzl0ver-firecamp/)
- Download *firecamp-service-cli* binary for the selected release (for example, *wget https://s3.amazonaws.com/jazzl0ver/firecamp/releases/1.0/packages/firecamp-service-cli.tgz*)
- Upon cluster upgrade completion, upgrade each service with the new binary (*firecamp-service-cli -op=upgrade-service* command)
- Start all services back
