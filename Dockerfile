FROM sdurrheimer/alpine-glibc

MAINTAINER Daniel Edwards <daniel.edwards@odfl.com> 

WORKDIR /

ADD ovirt-vm-prometheus-bridge .

ENTRYPOINT ["/ovirt-vm-prometheus-bridge"]
