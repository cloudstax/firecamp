
.PHONY: build docker test sources rpm

build:
	./scripts/install.sh

docker:
	./scripts/builddocker.sh

test:
	./scripts/gotest.sh

sources:
	cp $(GOPATH)/bin/openmanage-dockervolume openmanage-dockervolume
	cp packaging/openmanage-dockervolume/amazon-linux-ami/openmanage-dockervolume.spec openmanage-dockervolume.spec
	cp packaging/openmanage-dockervolume/amazon-linux-ami/openmanage-dockervolume.conf openmanage-dockervolume.conf
	tar -czf ./openmanage-dockervolume.tgz openmanage-dockervolume openmanage-dockervolume.conf

rpm: sources
	rpmbuild -bb packaging/openmanage-dockervolume/amazon-linux-ami/openmanage-dockervolume.spec

clean:
	-rm openmanage-dockervolume
	-rm openmanage-dockervolume.spec
	-rm openmanage-dockervolume.conf
	-rm openmanage-dockervolume.tgz
