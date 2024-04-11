// Code generated by private/model/cli/gen-api/main.go. DO NOT EDIT.

// Package opsworks provides the client and types for making API
// requests to AWS OpsWorks.
//
// Welcome to the AWS OpsWorks Stacks API Reference. This guide provides descriptions,
// syntax, and usage examples for AWS OpsWorks Stacks actions and data types,
// including common parameters and error codes.
//
// AWS OpsWorks Stacks is an application management service that provides an
// integrated experience for overseeing the complete application lifecycle.
// For information about this product, go to the AWS OpsWorks (http://aws.amazon.com/opsworks/)
// details page.
//
// # SDKs and CLI
//
// The most common way to use the AWS OpsWorks Stacks API is by using the AWS
// Command Line Interface (CLI) or by using one of the AWS SDKs to implement
// applications in your preferred language. For more information, see:
//
//   - AWS CLI (https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-welcome.html)
//
//   - AWS SDK for Java (https://docs.aws.amazon.com/AWSJavaSDK/latest/javadoc/com/amazonaws/services/opsworks/AWSOpsWorksClient.html)
//
//   - AWS SDK for .NET (https://docs.aws.amazon.com/sdkfornet/latest/apidocs/html/N_Amazon_OpsWorks.htm)
//
//   - AWS SDK for PHP 2 (https://docs.aws.amazon.com/aws-sdk-php-2/latest/class-Aws.OpsWorks.OpsWorksClient.html)
//
//   - AWS SDK for Ruby (http://docs.aws.amazon.com/sdkforruby/api/)
//
//   - AWS SDK for Node.js (http://aws.amazon.com/documentation/sdkforjavascript/)
//
//   - AWS SDK for Python(Boto) (http://docs.pythonboto.org/en/latest/ref/opsworks.html)
//
// # Endpoints
//
// AWS OpsWorks Stacks supports the following endpoints, all HTTPS. You must
// connect to one of the following endpoints. Stacks can only be accessed or
// managed within the endpoint in which they are created.
//
//   - opsworks.us-east-1.amazonaws.com
//
//   - opsworks.us-east-2.amazonaws.com
//
//   - opsworks.us-west-1.amazonaws.com
//
//   - opsworks.us-west-2.amazonaws.com
//
//   - opsworks.ca-central-1.amazonaws.com (API only; not available in the
//     AWS console)
//
//   - opsworks.eu-west-1.amazonaws.com
//
//   - opsworks.eu-west-2.amazonaws.com
//
//   - opsworks.eu-west-3.amazonaws.com
//
//   - opsworks.eu-central-1.amazonaws.com
//
//   - opsworks.ap-northeast-1.amazonaws.com
//
//   - opsworks.ap-northeast-2.amazonaws.com
//
//   - opsworks.ap-south-1.amazonaws.com
//
//   - opsworks.ap-southeast-1.amazonaws.com
//
//   - opsworks.ap-southeast-2.amazonaws.com
//
//   - opsworks.sa-east-1.amazonaws.com
//
// # Chef Versions
//
// When you call CreateStack, CloneStack, or UpdateStack we recommend you use
// the ConfigurationManager parameter to specify the Chef version. The recommended
// and default value for Linux stacks is currently 12. Windows stacks use Chef
// 12.2. For more information, see Chef Versions (https://docs.aws.amazon.com/opsworks/latest/userguide/workingcookbook-chef11.html).
//
// You can specify Chef 12, 11.10, or 11.4 for your Linux stack. We recommend
// migrating your existing Linux stacks to Chef 12 as soon as possible.
//
// See https://docs.aws.amazon.com/goto/WebAPI/opsworks-2013-02-18 for more information on this service.
//
// See opsworks package documentation for more information.
// https://docs.aws.amazon.com/sdk-for-go/api/service/opsworks/
//
// # Using the Client
//
// To contact AWS OpsWorks with the SDK use the New function to create
// a new service client. With that client you can make API requests to the service.
// These clients are safe to use concurrently.
//
// See the SDK's documentation for more information on how to use the SDK.
// https://docs.aws.amazon.com/sdk-for-go/api/
//
// See aws.Config documentation for more information on configuring SDK clients.
// https://docs.aws.amazon.com/sdk-for-go/api/aws/#Config
//
// See the AWS OpsWorks client OpsWorks for more
// information on creating client for this service.
// https://docs.aws.amazon.com/sdk-for-go/api/service/opsworks/#New
package opsworks