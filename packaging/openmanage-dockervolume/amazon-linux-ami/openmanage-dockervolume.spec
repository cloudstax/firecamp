# Copyright 2017 CloudStax.io, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the
# "License"). You may not use this file except in compliance
# with the License. A copy of the License is located at
#
#     http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is
# distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
# CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and
# limitations under the License.

Name:           openmanage-dockervolume
Version:        0.5
Release:        1%{?dist}
Vendor:         CloudStax.io
License:        Apache 2.0
Summary:        CloudStax docker volume driver

Source0:        openmanage-dockervolume.tgz
#Source1:        openmanage-dockervolume.conf

%global init_dir %{_sysconfdir}/init
%global bin_dir %{_libexecdir}

%description
openmanage-dockervolume is the docker volume driver on the openmanage platform.

%prep
%setup -c

%install
rm -rf $RPM_BUILD_ROOT
mkdir -p $RPM_BUILD_ROOT/%{init_dir}
mkdir -p $RPM_BUILD_ROOT/%{bin_dir}

#install %{SOURCE1} $RPM_BUILD_ROOT/%{init_dir}/openmanage-dockervolume.conf
install openmanage-dockervolume $RPM_BUILD_ROOT/%{bin_dir}/openmanage-dockervolume
install openmanage-dockervolume.conf $RPM_BUILD_ROOT/%{init_dir}/openmanage-dockervolume.conf

%files
%defattr(-,root,root,-)
%{init_dir}/openmanage-dockervolume.conf
%{bin_dir}/openmanage-dockervolume

%clean
rm -rf $RPM_BUILD_ROOT

%changelog
* Wed Jun 7 2017 <luo.junius@gmail.com> - 0.5-0
- Initial version
