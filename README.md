# sthlmshem-sok-forrad

Will search for free storage units in your house / area for Stockholms hem.

Should be configured as an AWS lambda that triggers on intervals. You will also need an SNS topic.

Configure following env vars on lambda

`PERSONNR`, `PASSWORD` and `TOPIC`.

## Build

Just run `make build` to create the lambda code package.

## TODOs

- Use KMS to encrypt `PERSONNR` and `PASSWORD`.
- Add Cloudformation to create all resources.
