# license-checker

`license-checker` is a command line tool that checks project licenses adhere to
a project rule file.

`license-checker` uses the
[github.com/google/licensecheck](www.github.com/google/licensecheck) library to
detect licenses in use, and uses a config file at
`<project-root>/license-checker.cfg` for the search rules and accepted licenses.

Example `license-checker.cfg` config:

```json
    {
        "paths":
        [
            { "exclude": [ "out/*", "build/*" ] },
            { "include": [ "out/foo.txt" ] }
        ],
        "licenses": [ "Apache-2.0-Header", "MIT" ]
    }
```

