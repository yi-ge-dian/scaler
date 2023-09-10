1. 安装docker
2. 安装kind
3. 安装k9s
4. 安装kubectl



5. 登陆

docker login --username=yi_ge_dian registry.cn-hangzhou.aliyuncs.com

6. 登出

docker logout

7. kind create cluster

8. kind load docker-image scaler:1.0

9. kubectl apply -f ../manifest/serverless-simulaion.yaml

10. kubectl delete -f ../manifest/serverless-simulaion.yaml
