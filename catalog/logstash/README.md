* [FireCamp Logstash Internals](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/logstash#firecamp-logstash-internals)
* [Tutorials](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/logstash#tutorials)

# FireCamp Logstash Internals

The FireCamp Logstash container is based on the offical elastic.co logstash image, docker.elastic.co/logstash/logstash:5.6.3. The data volume will be mounted to the /data directory inside container. The Logstash data will be stored under /data/logstash.

## Logstash

**Topology**

One Logstash member only runs in one availability zone. To tolerate one availability zone failure, the Logstash service could have multiple replicas. FireCamp will distribute the replicas evenly to availability zones.

**Memory**

By default, FireCamp sets 2GB for Logstash heap. As Logstash suggests, the Xms and Xmx are set to the same value to prevent the heap from resizing at runtime. You could adjust the memory size by setting the "ls-heap-size" when creating the service.

**Pipeline Config File**

FireCamp Logstash supports one config file, as Logstash currently has a single event pipeline. All configuration files are just concatenated (in order) as if you had written a single flat file. You should use "if" conditions to select the filters and outputs. See [Running multiple config files](https://discuss.elastic.co/t/running-multiple-independent-logstash-config-files-with-input-filter-and-output/29757), and [Multiple Logstash config files](https://discuss.elastic.co/t/solved-multiple-logstash-config-file/51692).

Please help to verify your config file is valid before creating the service. Could install logstash and run: /usr/share/logstash/bin/logstash -t -f your-logstash.conf.

**Pipeline tuning**

FireCamp Logstash uses the default Pipeline parameters. If you want to tune the parameters, could set them when creating the service. Updating the configs will be supported in the coming releases.

## Plugins

**Custom Plugins**

The CouchDB input plugin is supported as the example for how to support the custom plugin, catalog/logstash/5.6.3/dockerfile-input-couchdb. To use the CouchDB input plugin, specify -ls-container-image=jazzl0ver/firecamp-logstash-input-couchdb:5.6 at the service creation.

If the additional custom plugin is required, we could follow the same way to create a new docker image and specify it when creating the service. Note, this is currently disabled until we hear the actual requirement.

**Plugin Security**

Some plugins, such as the beats input plugin, supports SSL. This will be supported in the coming releases.

**Plugin Listen Port**

Logstash docker image exposes port 9600 and 5044. 9600 is used by Logstash metrics. 5044 is used by the beats plugin. If you use some plugin that requires to listen on another port, we could follow the same mechanism with the custom plugin, to create a new docker image to expose the port.

## Logging

The Logstash logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

1. [Performance Troubleshooting](https://www.elastic.co/guide/en/logstash/current/performance-troubleshooting.html)

2. [Tuning and Profiling Logstash Performance](https://www.elastic.co/guide/en/logstash/5.6/tuning-logstash.html)


# Tutorials

This is a simple tutorial about how to create a Logstash service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Logstash service name is "myls".

## Create CouchDB, ElasticSearch and Logstash services

Follow the [Installation Guide](https://github.com/jazzl0ver/firecamp/pkg/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones.

1. Create a CouchDB service and create "fruits" DB.
```
firecamp-service-cli -op=create-service -service-type=couchdb -region=us-east-1 -cluster=t1 -replicas=1 -volume-size=10 -service-name=mycouch -admin=admin -password=changeme
curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits
```
2. Create an ElasticSearch service:
```
firecamp-service-cli -op=create-service -service-type=elasticsearch -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=10 -service-name=myes -es-heap-size=2048 -es-disable-dedicated-master=true
```
3. Create a Logstash service:
```
firecamp-service-cli -op=create-service -service-type=logstash -region=us-east-1 -cluster=t1 -replicas=1 -volume-size=10 -service-name=myls -ls-pipeline-file=logstash.conf -ls-container-image=jazzl0ver/firecamp-logstash-input-couchdb:5.6 -ls-heap-size=2048
```
The logstash.conf content:
```
input {
  couchdb_changes {
    db => "fruits"
    host => "mycouch-0.t1-firecamp.com"
    port => "5984"
    username => "admin"
    password => "changeme"
  }
}

output {
  elasticsearch {
    hosts => ["myes-0.t1-firecamp.com:9200"]
    index => "couch_fruits"
  }
}
```

## Write to CouchDB and Read from ElasticSearch
Now the whole stack is setup. When the record is put into the "fruits" db in CouchDB, Logstash will read the record and put into the ElasticSearch service.

1. Write 2 records into CouchDB
```
curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits/001 -d '{ "item": "apple", "prices": 1.59 }'
curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits/002 -d '{ "item": "orange", "prices": 1.99 }'
```
2. Check Logstash pipeline status, `curl myls-0.t1-firecamp.com:9600/_node/stats/pipeline?pretty`
```
{
  "host" : "ip-172-31-66-230",
  "version" : "5.6.3",
  "http_address" : "myls-0.t1-firecamp.com:9600",
  "id" : "2219ec4a-828d-426e-a924-58268f94c3d9",
  "name" : "myls-0",
  "pipeline" : {
    "events" : {
      "duration_in_millis" : 322,
      "in" : 2,
      "out" : 2,
      "filtered" : 2,
      "queue_push_duration_in_millis" : 8
    },
    "plugins" : {
      "inputs" : [ {
        "id" : "34f9a9af49068429bb3ce14acd6ed414dc8ce380-1",
        "events" : {
          "out" : 2,
          "queue_push_duration_in_millis" : 8
        },
        "name" : "couchdb_changes"
      } ],
      "filters" : [ ],
      "outputs" : [ {
        "id" : "34f9a9af49068429bb3ce14acd6ed414dc8ce380-2",
        "events" : {
          "duration_in_millis" : 280,
          "in" : 2,
          "out" : 2
        },
        "name" : "elasticsearch"
      } ]
    },
    "reloads" : {
      "last_error" : null,
      "successes" : 0,
      "last_success_timestamp" : null,
      "last_failure_timestamp" : null,
      "failures" : 0
    },
    "queue" : {
      "type" : "memory"
    },
    "dead_letter_queue" : {
      "queue_size_in_bytes" : 2
    },
    "id" : "main"
  }
}
```
3. Query the ElasticSearch service, `curl myes-0.t1-firecamp.com:9200/couch_fruits/_search?pretty=true&q=*:*`
```
{
  "took" : 2,
  "timed_out" : false,
  "_shards" : {
    "total" : 5,
    "successful" : 5,
    "skipped" : 0,
    "failed" : 0
  },
  "hits" : {
    "total" : 5,
    "max_score" : 1.0,
    "hits" : [
      {
        "_index" : "couch_fruits",
        "_type" : "logs",
        "_id" : "AV9VvJ8Ey7UIfnP0rnZd",
        "_score" : 1.0,
        "_source" : {
          "@version" : "1",
          "doc" : {
            "prices" : 1.59,
            "item" : "apple"
          },
          "@timestamp" : "2017-10-25T22:52:25.470Z",
          "doc_as_upsert" : true
        }
      },
      {
        "_index" : "couch_fruits",
        "_type" : "logs",
        "_id" : "AV9VvLxxy7UIfnP0rnZo",
        "_score" : 1.0,
        "_source" : {
          "@version" : "1",
          "doc" : {
            "prices" : 1.99,
            "item" : "orange"
          },
          "@timestamp" : "2017-10-25T22:52:33.260Z",
          "doc_as_upsert" : true
        }
      },
      {
        "_index" : "couch_fruits",
        "_type" : "logs",
        "_id" : "AV9VyeX9y7UIfnP0rnnI",
        "_score" : 1.0,
        "_source" : {
          "@version" : "1",
          "doc" : {
            "prices" : 3.99,
            "item" : "pineapple"
          },
          "@timestamp" : "2017-10-25T23:06:55.856Z",
          "doc_as_upsert" : true
        }
      },
      {
        "_index" : "couch_fruits",
        "_type" : "logs",
        "_id" : "AV9VyNeqy7UIfnP0rnmA",
        "_score" : 1.0,
        "_source" : {
          "@version" : "1",
          "doc" : {
            "prices" : 1.59,
            "item" : "apple"
          },
          "@timestamp" : "2017-10-25T23:05:46.507Z",
          "doc_as_upsert" : true
        }
      },
      {
        "_index" : "couch_fruits",
        "_type" : "logs",
        "_id" : "AV9VyNety7UIfnP0rnmB",
        "_score" : 1.0,
        "_source" : {
          "@version" : "1",
          "doc" : {
            "prices" : 1.99,
            "item" : "orange"
          },
          "@timestamp" : "2017-10-25T23:05:46.542Z",
          "doc_as_upsert" : true
        }
      }
    ]
  }
}
```
