chmod +x ./out/cheer_proxy_http_linux
docker build -t hub.aiagain.com/vernus/cheer-proxy-http:prod .
docker push hub.aiagain.com/vernus/cheer-proxy-http:prod
