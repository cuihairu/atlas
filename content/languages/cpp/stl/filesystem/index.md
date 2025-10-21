---
title: 文件系统（std::filesystem）
---

核心类型
- `path`/`directory_entry`/`file_status`
- 迭代器：`directory_iterator`/`recursive_directory_iterator`

常用操作
```cpp
namespace fs = std::filesystem;
fs::path p = "/tmp/a.txt";
auto sz = fs::file_size(p);
for(auto& e : fs::directory_iterator("/tmp")){}
fs::create_directories("out/data");
fs::rename("old","new");
```

跨平台注意
- 路径分隔/编码：统一 UTF-8 入参，必要时做平台转换
- 权限：`status`/`perms` 查询与设置（POSIX/Windows 差异）
- 符号链接：递归遍历时注意环；用 `is_symlink` 与 `options::follow_directory_symlink`

最佳实践
- 避免在并行迭代中修改同一目录；
- I/O 错误使用 `std::error_code` 重载避免异常开销（在关键路径）
