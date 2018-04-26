#!/bin/bash
set -e

rootdir=/data
confdir=/data/conf
datadir=/data/kibana

# after release 0.9.5, these files are not created any more.
cfgfile=$confdir/kibana.yml
syscfgfile=$confdir/sys.conf

servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf

cfgdir=/usr/share/kibana/config

USER=kibana

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$rootdir" ]; then
  echo "error: $rootdir not exist. Please make sure the volume is mounted to $rootdir." >&2
  exit 1
fi
if [ ! -d "$datadir" ]; then
  mkdir "$datadir"
fi
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$1" = 'kibana' -a "$(id -u)" = '0' ]; then
  rootdiruser=$(stat -c "%U" $rootdir)
  if [ "$rootdiruser" != "$USER" ]; then
    echo "chown -R $USER $rootdir"
    chown -R "$USER" "$rootdir"
  fi
  diruser=$(stat -c "%U" $datadir)
  if [ "$diruser" != "$USER" ]; then
    chown -R "$USER" "$datadir"
  fi
  chown -R "$USER" "$confdir"

  exec gosu "$USER" "$BASH_SOURCE" "$@"
fi

if [ -f $servicecfgfile ]; then
  # after release 0.9.5
  . $servicecfgfile
  . $membercfgfile

  # overwrite kibana.yml file
  # https://www.elastic.co/guide/en/kibana/5.6/production.html
  # https://github.com/elastic/kibana-docker/tree/5.6/build/kibana/config
  echo "server.name: $MEMBER_NAME" > $cfgdir/kibana.yml
  echo "server.host: $BIND_IP" >> $cfgdir/kibana.yml
  echo "elasticsearch.url: $ES_URL" >> $cfgdir/kibana.yml
  echo "path.data: /data/kibana" >> $cfgdir/kibana.yml

  if [ "$ENABLE_SSL" = "true" ]; then
    echo "server.ssl.enabled: true" >> $cfgdir/kibana.yml
    echo "server.ssl.key: /data/conf/server.key" >> $cfgdir/kibana.yml
    echo "server.ssl.certificate: /data/conf/server.cert" >> $cfgdir/kibana.yml
  fi

  if [ -n "$PROXY_BASE_PATH" ]; then
    echo "server.basePath: $PROXY_BASE_PATH" >> $cfgdir/kibana.yml
  fi

  # disable xpack
  echo "xpack.security.enabled: false" >> $cfgdir/kibana.yml
  echo "xpack.monitoring.ui.container.elasticsearch.enabled: false" >> $cfgdir/kibana.yml

  # set "cpu.cgroup.path.override=/" and "cpuacct.cgroup.path.override=/".
  # Below are explanations copied from Kibana 5.6.3 docker start cmd, /usr/local/bin/kibana-docker.
  # The virtual file /proc/self/cgroup should list the current cgroup
  # membership. For each hierarchy, you can follow the cgroup path from
  # this file to the cgroup filesystem (usually /sys/fs/cgroup/) and
  # introspect the statistics for the cgroup for the given
  # hierarchy. Alas, Docker breaks this by mounting the container
  # statistics at the root while leaving the cgroup paths as the actual
  # paths. Therefore, Kibana provides a mechanism to override
  # reading the cgroup path from /proc/self/cgroup and instead uses the
  # cgroup path defined the configuration properties
  # cpu.cgroup.path.override and cpuacct.cgroup.path.override.
  # Therefore, we set this value here so that cgroup statistics are
  # available for the container this process will run in.
  echo "cpu.cgroup.path.override: /" >> $cfgdir/kibana.yml
  echo "cpuacct.cgroup.path.override: /" >> $cfgdir/kibana.yml

else
  # replace kibana.yml
  cp $cfgfile $cfgdir
  # load the sys config file
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
