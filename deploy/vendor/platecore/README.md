# PlateCore vendor drop

Private redistribution of the PlateCore native SDK used by the LPR
analytics module. The headers under `include/` are tracked in git so
the cgo wrapper compiles in CI without the binaries; the native
artefacts under `x86/` and the proxy binary are NOT in git (see
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
└── x86/
    └── lib/
        ├── libCore.so                     (NOT in git, linked as -lCore)
        ├── libonnxruntime.so.*            (NOT in git, dlopen'd at runtime)
        ├── libcrypto.so.*, libgomp.so.*
        ├── cuda/                          (CUDA runtime)
        └── models/                        (ONNX models)
```

`Dockerfile.vms-analytics` consumes this layout: the build stage links
against `x86/lib/libCore.so`, and the runtime stage copies the whole
`x86/lib/` tree into `/opt/platecore/lib/` plus the proxy binary into
`/opt/platecore/license-server-manager`. The cgo wrapper sets
`-Wl,-rpath,/opt/platecore/lib` so dlopen() resolves the .so chain
against that directory.
