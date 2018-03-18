#!/bin/sh
set -ex

region=us-east-1
cluster=t1
service=mycas

cqlsh ${service}-0.${cluster}-firecamp.com -u cassandra -p cassandra -e "CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';"
cqlsh ${service}-0.${cluster}-firecamp.com -u newsuperuser -p super -e "ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;"

cqlsh ${service}-0.${cluster}-firecamp.com -u newsuperuser -p super -e "CREATE KEYSPACE test WITH REPLICATION = { 'class' : 'NetworkTopologyStrategy', '$region' : 3 }; use test; CREATE TABLE users (userid text PRIMARY KEY, first_name text, last_name text); CREATE ROLE supervisor; GRANT MODIFY ON test.users TO supervisor; GRANT SELECT ON test.users TO supervisor; CREATE ROLE pam WITH PASSWORD = 'password' AND LOGIN = true; GRANT supervisor TO pam; LIST ALL PERMISSIONS OF pam;"

for i in `seq 1 10`
do
  cqlsh ${service}-2.${cluster}-firecamp.com -u pam -p password -e "use test; insert into users (userid, first_name, last_name) values('user$i', 'a$i', 'b$i');"
done
cqlsh ${service}-2.${cluster}-firecamp.com -u pam -p password -e "use test; select * from users;"
