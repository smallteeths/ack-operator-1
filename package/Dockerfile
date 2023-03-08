FROM registry.suse.com/bci/bci-base:15.4
RUN zypper -n update && \
    zypper -n clean -a && \
    rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*
RUN useradd --uid 1007 ack-operator
ENV KUBECONFIG /home/ack-operator/.kube/config
COPY bin/ack-operator /usr/bin/
USER 1007
ENTRYPOINT ["ack-operator"]
