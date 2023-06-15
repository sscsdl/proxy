# Proxy代理服务器

## Build构建二进制文件

- 编译当前系统环境的二进制文件
    ```shell
    go build main.go
    ```
- 在windows 64编译32位
    ```shell
    SET GOARCH=386
    go build main.go
    ```

- 在windows编译linux环境的二进制文件
```shell
SET CGO_ENABLED=0
SET GOOS=linux
SET GOARCH=amd64
go build main.go
```

- 或直接使用已编译好的二进制文件
    - Linux: `main-linux`
    - Windows: `main-windows.exe`

## Usage使用

- 普通代理服务器
    ```
    ./main-linux -model=0
    ```

- 主从代理服务器（从连接主）
    - 在主服务器中执行
        ```bash
        ./main-linux -model=1
        ```
    - 在从服务器中执行
        ```bash
        ./main-linux -model=2 -ip="master ip"
        ```