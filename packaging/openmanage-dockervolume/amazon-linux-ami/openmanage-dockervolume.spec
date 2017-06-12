Name:           openmanage-dockervolume
Version:        0.5
Release:        1%{?dist}
Vendor:         CloudStax
License:        Apache 2.0
Summary:        CloudStax docker volume driver

Source0:        openmanage-dockervolume.tgz

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
