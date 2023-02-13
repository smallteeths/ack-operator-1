FROM registry.suse.com/bci/bci-base:15.4
RUN zypper update -y && \
    zypper -n clean -a && \
    rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*
RUN useradd --uid 1007 ack-operator
ENV KUBECONFIG /home/ack-operator/.kube/config
ENV SSL_CERT_DIR /etc/rancher/ssl

COPY bin/ack-operator /usr/bin/
COPY package/entrypoint.sh /usr/bin
RUN chmod +x /usr/bin/entrypoint.sh

RUN mkdir -p /etc/rancher/ssl && \
    chown -R ack-operator /etc/rancher/ssl

USER 1007
ENTRYPOINT ["entrypoint.sh"]