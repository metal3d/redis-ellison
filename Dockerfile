FROM openshift/base-centos7

ENV GOPATH=/go
RUN set -xe; \
    yum install -y epel-release; \
    yum install -y golang git redis; \
    mkdir /go /go/bin /go/src /go/pkg; \
    fix-permissions /go; \
    go get -u github.com/metal3d/redis-ellison; \
    yum remove -y golang git; \
    yum clean all;\
    ln -s /go/bin/redis-ellison /usr/bin/redis-ellison;


EXPOSE 6379
CMD ["redis-ellison"]

