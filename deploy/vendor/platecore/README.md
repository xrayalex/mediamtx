# PlateCore vendor drop

Private redistribution of the PlateCore native SDK used by the LPR
analytics module. The headers under `include/` are tracked in git so
the cgo wrapper compiles in CI without the binaries; the per-arch
native artefacts and the proxy binary are NOT in git (see
`.gitignore`) and must be dropped in by hand before building the
analytics image.

Expected layout:

```
deploy/vendor/platecore/
├── README.md                              (in git)
├── .gitignore                             (in git)
├── include/platecore/
│   ├── platecore_api.h                    (in git)
│   └── platecore_types.h                  (in git)
├── license-server-manager_linux_amd64     (NOT in git, runtime proxy)
├── x86/                                   (NOT in git — PLATFORM_X86 build)
│   └── lib/
│       ├── libCore.so                     (linked as -lCore)
│       ├── libonnxruntime.so.*            (dlopen'd at runtime)
│       ├── libcrypto.so.*, libgomp.so.*
│       ├── cuda/                          (CUDA runtime)
│       └── models/                        (ONNX models)
└── aarch64/                               (NOT in git — PLATFORM_RK build, RK3588)
    ├── libCore.so                         (linked as -lCore on arm64)
    ├── librknnrt.so                       (Rockchip RKNN runtime, NPU on RK3588)
    ├── libcrypto.so*, libgomp.so*, libssl.so*
    └── models/                            (RKNN models)
```

`Dockerfile.vms-analytics` consumes the `x86/` layout for amd64
builds: the build stage links against `x86/lib/libCore.so`, the
runtime stage copies the whole `x86/lib/` tree into
`/opt/platecore/lib/`, plus the proxy binary into
`/opt/platecore/license-server-manager`. The cgo wrapper sets
`-Wl,-rpath,/opt/platecore/lib` so dlopen() resolves the .so chain
against that directory.

For RK3588 boards (PLATFORM_RK / aarch64), point the cgo `LDFLAGS`
at `aarch64/` instead and copy that tree to the runtime image. The
SDK auto-selects the NPU when `librknnrt.so` is present; the
`EngineConfig.GPU` selector is ignored on this platform.
