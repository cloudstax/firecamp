FROM confluentinc/cp-kafka-connect:4.0.0

# set the JVM TTL.
# https://www.confluent.io/blog/design-and-deployment-considerations-for-deploying-apache-kafka-on-aws/
RUN sed -i 's/#networkaddress.cache.ttl=-1/networkaddress.cache.ttl=10/g' /usr/lib/jvm/zulu-8-amd64/jre/lib/security/java.security


COPY firecamp-selectmember /
COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["/etc/confluent/docker/run"]
