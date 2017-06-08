# Copyright 2017 CloudStax.io, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the
# "License"). You may not use this file except in compliance
#  with the License. A copy of the License is located at
#
#     http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is
# distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
# CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and
# limitations under the License.

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
