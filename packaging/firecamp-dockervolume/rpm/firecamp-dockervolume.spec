Name:           firecamp-dockervolume
Version:        0.7
Release:        1
Vendor:         CloudStax
License:        Apache 2.0
Summary:        CloudStax FireCamp docker volume driver

Source0:        firecamp-dockervolume.tgz

%global init_dir %{_sysconfdir}/init
%global bin_dir %{_bindir}

%description
firecamp-dockervolume is the docker volume driver on the firecamp platform.

%prep
%setup -c

%install
rm -rf $RPM_BUILD_ROOT
mkdir -p $RPM_BUILD_ROOT/%{init_dir}
mkdir -p $RPM_BUILD_ROOT/%{bin_dir}

install firecamp-dockervolume $RPM_BUILD_ROOT/%{bin_dir}/firecamp-dockervolume
install firecamp-dockervolume.conf $RPM_BUILD_ROOT/%{init_dir}/firecamp-dockervolume.conf

%files
%defattr(-,root,root,-)
%{init_dir}/firecamp-dockervolume.conf
%{bin_dir}/firecamp-dockervolume

%clean
rm -rf $RPM_BUILD_ROOT

%changelog
* Wed Aug 24 2017 <junius@cloudstax.io> - 0.7-0
- rename to firecamp
* Wed Aug 1 2017 <junius@cloudstax.io> - 0.6-0
- fix the dns lookup wait
* Wed Jun 7 2017 <junius@cloudstax.io> - 0.5-0
- Initial version
