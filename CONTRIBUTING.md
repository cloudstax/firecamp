# Contributing

First off, thank you for considering contributing to FireCamp. We greatly value feedback and contributions from our community. Whether it's a new feature, correction, or additional documentation, we welcome your pull requests.

## Reporting issues
A great way to contribute to the project is to send a detailed report when you encounter an issue. We always appreciate a well-written, thorough bug report, and will thank you for it!

Check that the [current issues](https://github.com/jazzl0ver/firecamp/issues) don't already include that problem or suggestion before submitting an issue. If you find a match, you can use the "subscribe" button to get notified on updates. Do not leave random "+1" or "I have this too" comments, as they only clutter the discussion, and don't help resolving it. However, if you have ways to reproduce the issue or have additional information that may help resolving the issue, please leave a comment.

When reporting an issue, please always include the version, and the steps required to reproduce the problem if possible and applicable. This information will help us review and fix your issue faster. When sending lengthy log-files, consider posting them as a gist (https://gist.github.com). Don't forget to remove sensitive data from your logfiles before posting (you can replace those parts with "REDACTED").

## Pull requests are welcome
If you would like to make a significant change, it's a good idea to first open an issue to discuss it.

Please fork the repository and make changes on your fork in a feature branch:
* If it's a bug fix branch, name it XXXX-something where XXXX is the number of the issue.
* If it's a feature branch, create an enhancement issue to announce your intentions, and name it XXXX-something where XXXX is the number of the issue.

Note: The FireCamp is released under the [Apache 2.0 license](http://aws.amazon.com/apache-2-0/). Any code you submit will be released under that license.

## Testing
Any contribution should pass all tests. You could run the unit tests by `make test`. This requires `go` to be installed. The unit tests will operate on the AWS resources, such as creating an EC2 instance. You need to have the ability to access your AWS account.

Also it is recommended to install the cluster via CloudFormation and use the cli to create/delete a service, to make sure the change doesn't break anything.
