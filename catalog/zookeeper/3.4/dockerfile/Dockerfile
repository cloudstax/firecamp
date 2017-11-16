FROM openjdk:8-jre-alpine

# Install required packages
RUN apk add --no-cache \
  bash \
  su-exec

ENV ZOO_USER=zookeeper

RUN set -x \
  && adduser -D "$ZOO_USER"

ARG GPG_KEY=C823E3E5B12AF29C67F81976F5CECB3CB5E9BD2D
ARG DISTRO_NAME=zookeeper-3.4.10

# Download Apache Zookeeper, verify its PGP signature, untar and clean up
RUN set -x \
  && apk add --no-cache --virtual .build-deps \
     gnupg \
  && wget -q "http://www.apache.org/dist/zookeeper/$DISTRO_NAME/$DISTRO_NAME.tar.gz" \
  && wget -q "http://www.apache.org/dist/zookeeper/$DISTRO_NAME/$DISTRO_NAME.tar.gz.asc" \
  && export GNUPGHOME="$(mktemp -d)" \
  && gpg --keyserver ha.pool.sks-keyservers.net --recv-key "$GPG_KEY" \
  && gpg --batch --verify "$DISTRO_NAME.tar.gz.asc" "$DISTRO_NAME.tar.gz" \
  && tar -xzf "$DISTRO_NAME.tar.gz" \
  && rm -r "$GNUPGHOME" "$DISTRO_NAME.tar.gz" "$DISTRO_NAME.tar.gz.asc" \
  && apk del .build-deps

ENV PATH=$PATH:/$DISTRO_NAME/bin

# set the JVM TTL.
RUN sed -i 's/#networkaddress.cache.ttl=-1/networkaddress.cache.ttl=10/g' $JAVA_HOME/lib/security/java.security

COPY docker-entrypoint.sh /
ENTRYPOINT ["/docker-entrypoint.sh", "zkServer.sh", "start-foreground"]

EXPOSE 2181 2888 3888
