FROM debian:jessie-backports

RUN apt-get update && \
  apt-get install -t jessie-backports -y openjdk-8-jre-headless && \
  rm -rf /var/lib/apt/lists/*

ENV KM_VERSION=1.3.3.16 \
  KM_REVISION=6062369708ac7c968a307d7bde028b3282a629b9 \
  KM_CONFIGFILE="conf/application.conf"

RUN set -ex; \
  buildDeps='openjdk-8-jdk git wget unzip'; \
  apt-get update; \
  apt-get install -t jessie-backports -y --no-install-recommends $buildDeps; \
  mkdir -p /tmp; \
  cd /tmp; \
  git clone https://github.com/yahoo/kafka-manager; \
  cd /tmp/kafka-manager; \
  git checkout ${KM_REVISION}; \
  echo 'scalacOptions ++= Seq("-Xmax-classfile-name", "200")' >> build.sbt; \
  ./sbt clean dist; \
  unzip  -d / ./target/universal/kafka-manager-${KM_VERSION}.zip; \
  rm -fr /tmp/* /root/.sbt /root/.ivy2; \
  apt-get purge -y --auto-remove $buildDeps; \
  rm -rf /var/lib/apt/lists/*

WORKDIR /kafka-manager-${KM_VERSION}

COPY firecamp-service-updatedns /kafka-manager-${KM_VERSION}/
COPY docker-entrypoint.sh /kafka-manager-${KM_VERSION}/

EXPOSE 9000
ENTRYPOINT ["./docker-entrypoint.sh"]
