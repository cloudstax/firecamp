FROM cloudstax/firecamp-postgres:latest

RUN apt-get update \
	&& apt-get install -y postgis \
	&& rm -rf /var/lib/apt/lists/*
