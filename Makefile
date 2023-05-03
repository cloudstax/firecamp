
.PHONY: install docker test

org="jazzl0ver/"
version="1.6"
catalogversion="1.6"

all: install

cli:
	cd syssvc/firecamp-service-cli; go install; cd -
	cd $(GOPATH)/bin; tar -zcf firecamp-service-cli.tgz firecamp-service-cli; cd -

install:
	./scripts/install.sh

s3:
	./scripts/update-s3-bucket.sh $(org) $(version)

docker: install
	./scripts/builddocker.sh $(org) $(version) $(catalogversion) all

pluginimages:
	./scripts/builddocker.sh $(org) $(version) $(catalogversion) pluginimages

manageimages:
	./scripts/builddocker.sh $(org) $(version) $(catalogversion) manageimages

catalogimages:
	./scripts/builddocker.sh $(org) $(version) $(catalogversion) catalogimages

test:
	./scripts/gotest.sh

clean:
	-rm $(GOPATH)/bin/firecamp* || true
	-rm -fr build || true

cleanall: clean
	-rm -fr $(GOPATH)/pkg/linux_amd64/github.com/jazzl0ver/firecamp
