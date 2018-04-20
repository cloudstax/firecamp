#!/bin/dumb-init /bin/sh
set -e

# Note above that we run dumb-init as PID 1 in order to reap zombie processes
# as well as forward signals to all processes in its session. Normally, sh
# wouldn't do either of these functions so we'd leak zombies as well as do
# unclean termination of all our sub-processes.

rootdir=/data
datadir=/data/consul
confdir=/data/conf

# after release 0.9.5, the sys.conf is not created any more.
syscfgfile=$confdir/sys.conf

servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf
basiccfgfile=$confdir/basic_config.json

consulcfgdir=/consul/config

USER=consul

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
# sanity check to make sure the config files are created.
if [ ! -f "$basiccfgfile" ]; then
  echo "error: $basiccfgfile not exist." >&2
  exit 1
fi

# If we are running Consul, make sure it executes as the proper user.
if [ "$1" = 'consul' -a "$(id -u)" = '0' ]; then
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

  set -- gosu consul "$@"
fi

cp $basiccfgfile $consulcfgdir

if [ -f $servicecfgfile ]; then
  # after release 0.9.5, load service and member conf
  . $servicecfgfile
  . $membercfgfile

  # update basic_config.json
  # https://www.consul.io/docs/agent/options.html for details.
  sed -i 's/myDC/'$DC'/g' $consulcfgdir/basic_config.json
  sed -i 's/memberNodeName/'$MEMBER_NAME'/g' $consulcfgdir/basic_config.json
  sed -i 's/advertiseAddr/'$SERVICE_MEMBER'/g' $consulcfgdir/basic_config.json
  sed -i 's/bindAddr/'$BIND_IP'/g' $consulcfgdir/basic_config.json
  sed -i 's/myDomain/'$DOMAIN'/g' $consulcfgdir/basic_config.json

  # The secret key to use for encryption of Consul network traffic. This key must be 16-bytes that are Base64-encoded.
  # All nodes within a cluster must share the same encryption key to communicate.
  if [ -n "$ENCRYPT" ]; then
    echo "  \"encrypt\": \"$ENCRYPT\"," >> $consulcfgdir/basic_config.json
  fi

  if [ "$ENABLE_TLS" = "true" ]; then
    echo '  "key_file": "/data/conf/key.pem",' >> $consulcfgdir/basic_config.json
    echo '  "cert_file": "/data/conf/cert.pem",' >> $consulcfgdir/basic_config.json
    echo '  "ca_file": "/data/conf/ca.crt",' >> $consulcfgdir/basic_config.json
    echo '  "ports": {' >> $consulcfgdir/basic_config.json
    echo '    "https": '$HTTPS_PORT'' >> $consulcfgdir/basic_config.json
    echo '  },' >> $consulcfgdir/basic_config.json
    echo '  "verify_incoming": true,' >> $consulcfgdir/basic_config.json
    echo '  "verify_incoming_rpc": true,' >> $consulcfgdir/basic_config.json
    echo '  "verify_incoming_https": true,' >> $consulcfgdir/basic_config.json
    echo '  "verify_outgoing": true,' >> $consulcfgdir/basic_config.json
  fi

  # add footer
  echo '  "log_level": "INFO",' >> $consulcfgdir/basic_config.json
  echo '  "ui": true' >> $consulcfgdir/basic_config.json
  echo '}' >> $consulcfgdir/basic_config.json

else
  # before release 0.9.6, load the sys config file
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
