# wolfictl vet

Run a local "pre-flight" vetting pipeline for a **melange** or **apko** manifest before opening a Pull Request.  
The command bundles the most common CI checks (formatting, linting, update detection, optional build+scan) into a single, fast CLI workflow, giving quick feedback and saving CI minutes.

---

## Synopsis

```
wolfictl vet [flags] MANIFEST.yaml [MANIFEST.yaml ...]
```

For every supplied manifest the command:

1. Detects the manifest type (melange vs apko).
2. Runs **format** validation (`wolfictl lint yam`) to ensure canonical YAML layout.
3. Runs **lint** rules (`wolfictl lint`) and fails on any rule with *error* severity.
4. (future) Runs **check update** validation once the sub-command becomes available.
5. *Optional* – executes the full build & scan pipeline:
   * **melange**  
     - Generates signing keys if absent  
     - `melange build` for the current architecture  
     - Exports produced `.apk` files to `--temp-dir`  
     - Scans every APK with **Grype/Trivy**
   * **apko**  
     - `terraform fmt` on accompanying Terraform files  
     - `apko build` to an OCI tarball in `--temp-dir`  
     - Scans the image tarball with **Grype/Trivy**

The command exits non-zero on the first failure and prints a concise error.

---

## Options

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--run-melange-pipeline` | `false` | After linting, run the melange build & scan pipeline (only relevant for melange manifests). |
| `--run-apko-pipeline` | `false` | After linting, run the apko build & scan pipeline (only relevant for apko manifests). |
| `--temp-dir` | system temp dir | Directory used for intermediate build artifacts and scan outputs. |
| `--verbose` | `false` | Print underlying command output and additional debug information. |
| `-h, --help` |   | Show help for `vet`. |

---

## Examples

Run basic checks on a single manifest:

```
wolfictl vet my-package.yaml
```

Run the full melange build + scan locally (useful before pushing large changes):

```
wolfictl vet --run-melange-pipeline packages/my-package.yaml
```

Lint several manifests in one go:

```
wolfictl vet pkg1.yaml pkg2.yaml pkg3.yaml
```

Store build artifacts in a custom directory and enable verbose logging:

```
wolfictl vet --run-apko-pipeline --temp-dir ./artifacts --verbose images/webserver.yaml
```

---

## Exit Codes

| Code | Meaning |
| ---- | ------- |
| `0`  | All requested checks passed. |
| `>0` | One or more checks failed. Review the console output for details. |

---

## See Also

* `wolfictl lint` ‑ lint rules used internally by `vet`.  
* `wolfictl lint yam` ‑ YAML formatter invoked by `vet`.  
* [`melange`](https://github.com/chainguard-dev/melange) & [`apko`](https://github.com/chainguard-dev/apko) – build tools executed when the respective pipeline flags are enabled.  
