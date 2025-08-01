


 aws s3 sync . s3://obsidian-sync-test-abcd/ --dryrun
 aws s3 sync s3://obsidian-sync-test-abcd/ . --dryrun --delete


aws s3 sync /path/to/local/folder s3://your-bucket-name/folder --dryrun
aws s3 sync s3://your-bucket-name/folder /path/to/local/folder --dryrun

<!-- replace github.com/seqsense/s3sync => github.com/cjh-cloud/s3sync v0.0.0-20241004041939-0947a5aa1293 -->

# Things to do
- [ ] tests
- [ ] build pipeline for linux and mac
- [ ] aws command to create user and role (perms in a JSON file?) and generate creds?
- [ ] auto updating? - release to github, check if newer version on startup
- [ ] aws cost explorer info
- [x] aws credentials - instead of default, use config values (or change to profile?)
- [ ] Sync needs to use aws creds above - might be better just to pass in aws config thing

scrolling function on bottom part
first run, just say loading, last just 1 sec

handle dynamo/s3 requests better - 2025/06/29 18:54:54 Failed to scan DynamoDB table: operation error DynamoDB: Scan, exceeded maximum number of attempts, 3, https response error StatusCode: 0, RequestID: , request send failed, Post "https://dynamodb.ap-southeast-2.amazonaws.com/": dial tcp: lookup dynamodb.ap-southeast-2.amazonaws.com on 127.0.0.53:53: server misbehaving

# Infra
- Would Pulumi be better? Could Go generate everything it needed then?

## Terraform
tf init

