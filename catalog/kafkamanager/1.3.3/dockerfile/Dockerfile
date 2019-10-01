FROM kafkamanager/kafka-manager:1.3.3.22

ENV KM_VERSION=1.3.3.22 \
  KM_CONFIGFILE="conf/application.conf"


WORKDIR /opt/kafka-manager-${KM_VERSION}

COPY logback.xml conf/
COPY firecamp-selectmember .
COPY docker-entrypoint.sh .

EXPOSE 9000
ENTRYPOINT ["./docker-entrypoint.sh"]
