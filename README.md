


 aws s3 sync . s3://obsidian-sync-test-abcd/ --dryrun
 aws s3 sync s3://obsidian-sync-test-abcd/ . --dryrun --delete


aws s3 sync /path/to/local/folder s3://your-bucket-name/folder --dryrun
aws s3 sync s3://your-bucket-name/folder /path/to/local/folder --dryrun

# Things to do
- [ ] tests
- [ ] build pipeline for linux and mac
- [ ] aws command to create user and role (perms in a JSON file?) and generate creds?
- [ ] auto updating? - release to github, check if newer version on startup
- [ ] aws cost explorer info
- [x] aws credentials - instead of default, use config values (or change to profile?)
- [ ] Sync needs to use aws creds above - might be better just to pass in aws config thing

# Infra
- Would Pulumi be better? Could Go generate everything it needed then?

## Terraform
tf init

