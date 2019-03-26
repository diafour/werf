{% if include.header %}
{% assign header = include.header %}
{% else %}
{% assign header = "###" %}
{% endif %}
Encrypt data from FILE_PATH or pipe.
Encryption key should be in $WERF_SECRET_KEY or .werf_secret_key file

{{ header }} Syntax

```bash
werf helm secret file encrypt [FILE_PATH] [options]
```

{{ header }} Examples

```bash
  # Encrypt and save result in file
  $ werf helm secret file encrypt tls.crt -o .helm/secret/tls.crt
```

{{ header }} Environments

```bash
  $WERF_SECRET_KEY  Use specified secret key to extract secrets for the deploy; recommended way to 
                    set secret key in CI-system
```

{{ header }} Options

```bash
      --dir='':
            Change to the specified directory to find werf.yaml config
  -h, --help=false:
            help for encrypt
      --home-dir='':
            Use specified dir to store werf cache files and dirs (default $WERF_HOME or ~/.werf)
  -o, --output-file-path='':
            Write to file instead of stdout
      --tmp-dir='':
            Use specified dir to store tmp files and dirs (default $WERF_TMP_DIR or system tmp dir)
```
