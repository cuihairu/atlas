---
title: GitHub Actions 矩阵（Linux/macOS/Windows + GCC/Clang/MSVC）
---

最小工作流（摘要）
```yaml
name: ci
on: [push, pull_request]
jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        compiler: [gcc, clang, msvc]
        exclude:
          - os: macos-latest
            compiler: msvc
          - os: windows-latest
            compiler: gcc
          - os: windows-latest
            compiler: clang
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - name: Setup CMake
        uses: jwlawson/actions-setup-cmake@v1
      - name: Configure
        run: |
          cmake -B build -S . -DCMAKE_BUILD_TYPE=Release
      - name: Build
        run: cmake --build build -j
      - name: Test
        run: ctest --test-dir build --output-on-failure || true
```

提示
- 生产 CI 建议加 vcpkg/Conan 的缓存与工具链、ASan/TSan 独立任务、格式/静态分析。
