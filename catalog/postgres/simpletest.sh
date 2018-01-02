#!/bin/sh
set -ex

PGPASSWORD=changeme psql -h mypg-0.t1-firecamp.com -U postgres -c "CREATE USER tester WITH PASSWORD 'password';"
PGPASSWORD=changeme psql -h mypg-0.t1-firecamp.com -U postgres -c "CREATE DATABASE testdb;"
PGPASSWORD=changeme psql -h mypg-0.t1-firecamp.com -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE testdb to tester;"

PGPASSWORD=password psql -h mypg-0.t1-firecamp.com -d testdb -U tester -c "CREATE TABLE guestbook (visitor_email text, vistor_id serial, date timestamp, message text);"
for i in `seq 1 10`
do
  PGPASSWORD=password psql -h mypg-0.t1-firecamp.com -d testdb -U tester -c "INSERT INTO guestbook (visitor_email, date, message) VALUES ( 'jim-$i@gmail.com', current_date, 'jim $i');"
done

PGPASSWORD=password psql -h mypg-0.t1-firecamp.com -d testdb -U tester -c "SELECT * FROM guestbook;"

