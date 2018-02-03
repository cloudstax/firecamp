FROM %%OrgName%%firecamp-postgres:9.6

RUN apt-get update \
	&& apt-get install -y postgis \
	&& rm -rf /var/lib/apt/lists/*
