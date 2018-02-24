#!/bin/sh
set -ex

cluster=t1

curl -XGET "myes-master-0.${cluster}-firecamp.com:9200/_cluster/health?pretty"
curl -XGET "http://myes-0.${cluster}-firecamp.com:9200/_cluster/state"

curl -XPUT "http://myes-0.${cluster}-firecamp.com:9200/twitter/doc/1?pretty" -H 'Content-Type: application/json' -d '
{
    "user": "kimchy",
    "post_date": "2009-11-15T13:12:00",
    "message": "Trying out Elasticsearch, so far so good?"
}'

curl -XPUT "http://myes-1.${cluster}-firecamp.com:9200/twitter/doc/2?pretty" -H 'Content-Type: application/json' -d '
{
    "user": "kimchy",
    "post_date": "2009-11-15T14:12:12",
    "message": "Another tweet, will it be indexed?"
}'

curl -XPUT "http://myes-2.${cluster}-firecamp.com:9200/twitter/doc/3?pretty" -H 'Content-Type: application/json' -d '
{
    "user": "elastic",
    "post_date": "2010-01-15T01:46:38",
    "message": "Building the site, should be kewl"
}'

curl -XGET "http://myes-2.${cluster}-firecamp.com:9200/twitter/doc/1?pretty=true"
curl -XGET "http://myes-1.${cluster}-firecamp.com:9200/twitter/doc/2?pretty=true"
curl -XGET "http://myes-0.${cluster}-firecamp.com:9200/twitter/doc/3?pretty=true"


curl -XGET "http://myes-0.${cluster}-firecamp.com:9200/twitter/_search?q=user:kimchy&pretty=true"
curl -XGET "http://myes-1.${cluster}-firecamp.com:9200/twitter/_search?pretty=true" -H 'Content-Type: application/json' -d '
{
    "query" : {
        "match" : { "user": "kimchy" }
    }
}'
