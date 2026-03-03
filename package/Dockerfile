FROM registry.suse.com/bci/bci-base:15.7

ARG TARGETARCH
RUN zypper update -y && \
    zypper in -y -f vim && \
    zypper clean -a && \
    rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*

RUN useradd --uid 1007 ack-operator
ENV KUBECONFIG /home/ack-operator/.kube/config
COPY dist/ /usr/bin/
USER 1007
ENTRYPOINT ["ack-operator"]
