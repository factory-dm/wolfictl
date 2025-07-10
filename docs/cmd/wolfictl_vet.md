# `wolfictl vet`

Run a local **vetting pipeline** for Wolfi _melange_ or _apko_ manifests before opening a pull-request.  
The command bundles the most common pre-CI checks into one step so you can catch issues quickly without waiting for GitHub Actions.

## What it does

For the supplied manifest the pipeline will, in order:

1. **Identify** whether the manifest is a _melange_ build recipe or an _apko_ image definition.
2. **Format check** – runs `wolfictl lint yam` to verify YAML formatting.
3. **Lint check** – runs `wolfictl lint` to apply Wolfi-specific lint rules.
4. **Update check** – (placeholder) validates package versions are up-to-date.
5. **Optional build & scan** (`--run-build`)  
   • _melange_: generates keys (if missing), performs `melange build`, exports the produced `.apk`, then scans it.  
   • _apko_: optionally `terraform fmt`, executes `apko build`, then scans the resulting image or tarball.  
6. **Security scan** – uses Grype (default) or Trivy to report CVEs on the built artefact.

The vet command **stops at the first failure** so you can address issues incrementally.

## Usage

```
wolfictl vet [flags] <manifest.yaml>
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--run-build` | `false` | Enable the build + security scan portion of the pipeline. |
| `--scanner`   | `grype` | Security scanner to use (`grype` or `trivy`). |
| `--melange-key-dir` | `~/.melange` | Directory where `melange keygen` stores signing keys (created if missing). |
| `--apko-config` | _empty_ | Optional apko configuration file passed through to `apko build`. |
| `--log-level` | `warn` | (global) Set log verbosity for all wolfictl commands. |

## Examples

Basic vetting (format + lint + update):

```
wolfictl vet configs/nginx.melange.yaml
```

Run the full melange build and scan with Trivy:

```
wolfictl vet --run-build --scanner trivy manifests/nginx.melange.yaml
```

Vetting an apko manifest, letting vet handle `terraform fmt` automatically:

```
wolfictl vet --run-build images/webserver.apko.yaml
```

Using a custom key directory and apko config:

```
wolfictl vet --run-build \
  --melange-key-dir ~/.cache/melange/keys \
  --apko-config apko.build.yaml \
  manifests/my-image.apko.yaml
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | All checks (and optional build) succeeded. |
| `>0` | The first stage that failed; the command stops at that point. |

## See also

* [`wolfictl lint`](./wolfictl_lint.md) – linter invoked by the vet pipeline  
* [`wolfictl lint yam`](./wolfictl_lint_yam.md) – YAML formatter check  
* [`melange`](https://github.com/chainguard-dev/melange) – package build tool  
* [`apko`](https://github.com/chainguard-dev/apko) – container image build tool  
* [Wolfi OS workflows](https://github.com/wolfi-dev/os/tree/main/.github/workflows) – CI reference implementation

---

_Introduced in FAC-24_
