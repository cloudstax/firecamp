#!/bin/sh
set -ex


# for the single replicaset
primary=$(mongo --host mymongo-0.t1-firecamp.com --eval "db.isMaster()" | grep primary | awk '{ print $3 }' | awk -F "\"" '{ print $2 }')
# for the sharded cluster
#primary=localhost

mongo --host $primary -u admin -p changeme --authenticationDatabase admin memberdb --eval "
db.createUser(
  {
    user: 'u1',
    pwd: 'u1',
    roles: [
      { role: 'readWrite', db: 'memberdb' }
    ]
  }
)"

for i in `seq 1 10`; do
  mongo --host $primary -u u1 -p u1 --authenticationDatabase memberdb memberdb --eval "
    db.members.insertOne(
      {
        name: 'sue$i',
        age: $i,
        status: 'P',
        likes : [ 'golf', 'football' ]
      }
    )
  "
done

mongo --host $primary -u u1 -p u1 --authenticationDatabase memberdb memberdb --eval "db.members.find()"
