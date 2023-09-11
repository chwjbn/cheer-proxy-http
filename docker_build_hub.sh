chmod +x ./out/cheer_socks_linux
docker build -t hub.aiagain.com/vernus/cheersocks:prod .
docker push hub.aiagain.com/vernus/cheersocks:prod
