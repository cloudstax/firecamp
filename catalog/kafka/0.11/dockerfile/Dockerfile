FROM openjdk:8-jre-alpine

# Install required packages
RUN apk add --no-cache \
  bash \
  su-exec

ENV KAFKA_USER=kafka

RUN set -x \
  && adduser -D "$KAFKA_USER"

ENV KAFKA_VERSION 0.11.0.0
ENV KAFKA_SCALA_VERSION 2.12
ENV KAFKA_RELEASE kafka_${KAFKA_SCALA_VERSION}-${KAFKA_VERSION}

# Download Kafka Zookeeper, verify its digest integrity, untar and clean up
RUN set -x \
  && wget -q http://www.us.apache.org/dist/kafka/${KAFKA_VERSION}/${KAFKA_RELEASE}.tgz \
  && tar -zx -C / -f ${KAFKA_RELEASE}.tgz \
  && mv /${KAFKA_RELEASE} /kafka \
  && chown -R $KAFKA_USER /kafka \
  && rm -f ${KAFKA_RELEASE}.tgz

ENV PATH=$PATH:/kafka/bin

COPY docker-entrypoint.sh /
ENTRYPOINT ["/docker-entrypoint.sh", "kafka-server-start.sh", "/kafka/config/server.properties"]

EXPOSE 9092
