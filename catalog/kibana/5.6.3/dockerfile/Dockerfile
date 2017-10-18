FROM docker.elastic.co/elasticsearch/elasticsearch:5.6.3

WORKDIR /

USER root

ENV PATH /usr/share/elasticsearch/bin:/usr/share/kibana/bin:$PATH

# remove the ingest plugins that are not required for kibana
RUN for PLUGIN in ingest-user-agent ingest-geoip; do \
      elasticsearch-plugin remove "$PLUGIN"; \
    done


# https://github.com/elastic/kibana-docker/blob/5.6/templates/Dockerfile.j2
# Add Reporting dependencies.
RUN yum update -y && yum install -y fontconfig freetype && yum clean all

WORKDIR /usr/share/kibana

# Download and extract defined kibana version.
RUN curl -fsSL https://artifacts.elastic.co/downloads/kibana/kibana-5.6.3-linux-x86_64.tar.gz | \
      tar --strip-components=1 -zxf - && \
      ln -s /usr/share/kibana /opt/kibana

RUN kibana-plugin install x-pack

ENV GOSU_VERSION 1.10
RUN set -ex; \
	\
	yum -y install epel-release; \
	yum -y install wget dpkg; \
	\
	dpkgArch="$(dpkg --print-architecture | awk -F- '{ print $NF }')"; \
	wget -O /usr/bin/gosu "https://github.com/tianon/gosu/releases/download/$GOSU_VERSION/gosu-$dpkgArch"; \
	wget -O /tmp/gosu.asc "https://github.com/tianon/gosu/releases/download/$GOSU_VERSION/gosu-$dpkgArch.asc"; \
	\
# verify the signature
	export GNUPGHOME="$(mktemp -d)"; \
	gpg --keyserver ha.pool.sks-keyservers.net --recv-keys B42F6819007F00F88E364FD4036A9C25BF357DD4; \
	gpg --batch --verify /tmp/gosu.asc /usr/bin/gosu; \
	rm -r "$GNUPGHOME" /tmp/gosu.asc; \
	\
	chmod +x /usr/bin/gosu; \
# verify that the binary works
	gosu nobody true; \
	\
	yum -y remove wget dpkg; \
	yum clean all

WORKDIR /

# set the JVM TTL.
RUN sed -i 's/#networkaddress.cache.ttl=-1/networkaddress.cache.ttl=10/g' $JAVA_HOME/lib/security/java.security

EXPOSE 9200 9300 5601

COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]
