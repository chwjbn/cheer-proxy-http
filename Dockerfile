FROM alpine
WORKDIR /data/app/
COPY ./out/ /data/app/
CMD ["/data/app/cheer_socks_linux"]