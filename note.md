### 同步官方代码
```shell
git remote add upstream https://github.com/siyuan-note/siyuan.git
git remote -v

git fetch upstream
git checkout master
git merge upstream/master
```

### 构建docker image
```shell
docker build --build-arg NPM_REGISTRY=https://registry.npmmirror.com -t gavin/siyuan-custom:dev .
```
