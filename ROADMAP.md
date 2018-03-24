# ROADMAP

The ultimate goal is to connect the open source Stateful Services with FaaS, such as AWS Lambda, Apache OpenWhisk, etc. So data could easily move from application to stateful service, from one stateful service to another stateful service, or from stateful service to application.

The release plans are briefly described here. For the full list of features and fixes of one release, please refer to the [release docs](https://github.com/cloudstax/firecamp/tree/master/docs/releases).

The plan will be adjusted by the requirements. If you prefer to have some other features, please feel free to open an issue or join slack to propose your idea.


# 0.9.5

## Must-have features

* Initial Monitoring support for Cassandra, Kafka, ZooKeeper & Redis
* Support Upgrade from 0.9.4

## Other features

* Initial work to connect Kafka to ElasticSearch

# 0.9.6

## Must-have features

* Security: add SSL/TLS for FireCamp management service
* Monitor more services
* Further enhance upgrade

## Other features

* Connect Kafka to ElasticSearch
* Further enhance Kubernetes support


# Further works

The near term features will be included in the coming releases. The detail plan will be finalized soon.

## Near term features

* Service Scale: Kafka, ZooKeeper, etc
* Fully support Kubernetes on AWS
* Data Management: data snapshot, backup, recovery
* Automatic Data Pipelines
* Support Microsoft Azure, Google Cloud Platform
* Support on-premise Kubernetes
* Support more stateful services

## Long term features

* Geo Access and Protection
* Support open source FaaS engines, such as Apache OpenWhisk
* Connect stateful services with FaaS
* Automatic Data Workflow
* Hybrid Cloud

