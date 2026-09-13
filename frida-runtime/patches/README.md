# Local Frida Patches

Keep each local source change as one numbered patch in this directory. The
upstream checkout remains pinned in `../source.lock.json`; rebuild all Android
ABIs after applying a patch:

```sh
./scripts/build-frida.sh
./scripts/build-module.sh
```

Do not edit generated `build/` outputs. They are build products only.
