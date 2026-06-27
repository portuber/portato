FROM debian:12

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    systemd systemd-sysv dbus libpam-systemd \
    openssh-server python3 netcat-openbsd ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash appuser && echo 'appuser:appuser' | chpasswd

RUN ssh-keygen -A \
    && install -d -o appuser -g appuser -m 700 /home/appuser/.ssh \
    && ssh-keygen -t ed25519 -N '' -f /home/appuser/.ssh/id_ed25519 -C appuser-test \
    && cp /home/appuser/.ssh/id_ed25519.pub /home/appuser/.ssh/authorized_keys \
    && touch /home/appuser/.ssh/known_hosts \
    && chown -R appuser:appuser /home/appuser/.ssh \
    && chmod 600 /home/appuser/.ssh/id_ed25519 /home/appuser/.ssh/authorized_keys /home/appuser/.ssh/known_hosts

RUN install -d -o appuser -g appuser -m 700 /home/appuser/.config/portato
COPY portato-config.yaml /home/appuser/.config/portato/config.yaml
RUN chown -R appuser:appuser /home/appuser/.config
RUN chown appuser:appuser /home/appuser/.config/portato/config.yaml && chmod 600 /home/appuser/.config/portato/config.yaml

COPY portato /usr/local/bin/portato
RUN chmod 0755 /usr/local/bin/portato

COPY e2e.sh /e2e/e2e.sh
RUN chmod 0755 /e2e/e2e.sh

RUN ln -sf /dev/null /etc/systemd/system/ssh.socket \
    && ln -sf /dev/null /etc/systemd/system/ssh.service
COPY sshd-test.service /etc/systemd/system/sshd-test.service
COPY echo.service       /etc/systemd/system/echo.service
COPY enable-linger.service /etc/systemd/system/enable-linger.service
RUN mkdir -p /etc/systemd/system/multi-user.target.wants \
    && ln -sf /etc/systemd/system/sshd-test.service    /etc/systemd/system/multi-user.target.wants/sshd-test.service \
    && ln -sf /etc/systemd/system/echo.service          /etc/systemd/system/multi-user.target.wants/echo.service \
    && ln -sf /etc/systemd/system/enable-linger.service /etc/systemd/system/multi-user.target.wants/enable-linger.service

CMD ["/sbin/init"]