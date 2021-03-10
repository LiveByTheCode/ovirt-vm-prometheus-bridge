FROM sdurrheimer/alpine-glibc

MAINTAINER Daniel Edwards <daniel.edwards@odfl.com> 

ENV VERSION v1.0.0 

ENV NAME ovirt-vm-prometheus-bridge-$VERSION

ENV SOURCE_URL https://github.com/livebythecode/ovirt-vm-prometheus-bridge/releases/download/$VERSION/$NAME.tar.gz

RUN apk add --update curl && cd / && curl -L -O $SOURCE_URL && gunzip $NAME.tar.gz && tar xf $NAME.tar && mv $NAME/bin/ovirt-prometheus-bridge / && rm -rf $NAME.tar.gz $NAME && apk del curl && rm -rf /var/cache/apk/*


ENTRYPOINT ["/ovirt-vm-prometheus-bridge"]
