package main

import (
	"archive/zip"
	"os"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudfront"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudfrontorigins"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsdynamodb"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

type SwimStackProps struct {
	awscdk.StackProps
	Env string
}

func NewSwimStack(scope constructs.Construct, id string, props *SwimStackProps) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &props.StackProps)
	env := props.Env
	sfx := "-" + env

	// ─── DynamoDB ────────────────────────────────────────────────────────────

	table := awsdynamodb.NewTable(stack, jsii.String("Table"), &awsdynamodb.TableProps{
		TableName: jsii.String("swim-app" + sfx),
		PartitionKey: &awsdynamodb.Attribute{
			Name: jsii.String("pk"),
			Type: awsdynamodb.AttributeType_STRING,
		},
		SortKey: &awsdynamodb.Attribute{
			Name: jsii.String("sk"),
			Type: awsdynamodb.AttributeType_STRING,
		},
		BillingMode:         awsdynamodb.BillingMode_PAY_PER_REQUEST,
		TimeToLiveAttribute: jsii.String("ttl"),
		RemovalPolicy:       removalPolicy(env),
	})

	table.AddGlobalSecondaryIndex(&awsdynamodb.GlobalSecondaryIndexProps{
		IndexName: jsii.String("swimmerEmail-index"),
		PartitionKey: &awsdynamodb.Attribute{
			Name: jsii.String("swimmerEmail"),
			Type: awsdynamodb.AttributeType_STRING,
		},
		ProjectionType: awsdynamodb.ProjectionType_ALL,
	})

	// ─── Lambda ──────────────────────────────────────────────────────────────

	lambdaEnv := map[string]*string{
		"TABLE_NAME": table.TableName(),
	}
	if v := os.Getenv("GOOGLE_CALENDAR_ID"); v != "" {
		lambdaEnv["GOOGLE_CALENDAR_ID"] = jsii.String(v)
	}

	fn := awslambda.NewFunction(stack, jsii.String("Api"), &awslambda.FunctionProps{
		FunctionName: jsii.String("swim-signup-api" + sfx),
		Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
		Architecture: awslambda.Architecture_ARM_64(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String("../backend/lambda.zip"), nil),
		Timeout:      awscdk.Duration_Seconds(jsii.Number(30)),
		MemorySize:   jsii.Number(256),
		Environment:  &lambdaEnv,
	})

	table.GrantReadWriteData(fn)

	// ─── Lambda Function URL ──────────────────────────────────────────────────

	fnUrl := fn.AddFunctionUrl(&awslambda.FunctionUrlOptions{
		AuthType: awslambda.FunctionUrlAuthType_NONE,
	})

	awslambda.NewCfnPermission(stack, jsii.String("AllowPublicUrl"), &awslambda.CfnPermissionProps{
		Action:              jsii.String("lambda:InvokeFunctionUrl"),
		FunctionName:        fn.FunctionArn(),
		Principal:           jsii.String("*"),
		FunctionUrlAuthType: jsii.String("NONE"),
	})

	awslambda.NewCfnPermission(stack, jsii.String("AllowPublicInvoke"), &awslambda.CfnPermissionProps{
		Action:    jsii.String("lambda:InvokeFunction"),
		FunctionName: fn.FunctionArn(),
		Principal: jsii.String("*"),
	})

	// ─── S3 (UI) ─────────────────────────────────────────────────────────────

	bucketPrefix := os.Getenv("BUCKET_PREFIX")
	if bucketPrefix == "" {
		panic("BUCKET_PREFIX env var is required")
	}

	uiBucket := awss3.NewBucket(stack, jsii.String("UiBucket"), &awss3.BucketProps{
		BucketName:           jsii.String(bucketPrefix + sfx),
		WebsiteIndexDocument: jsii.String("index.html"),
		WebsiteErrorDocument: jsii.String("index.html"),
		PublicReadAccess:     jsii.Bool(true),
		BlockPublicAccess:    awss3.BlockPublicAccess_BLOCK_ACLS(),
		AutoDeleteObjects:    jsii.Bool(env != "prod"),
		RemovalPolicy:        removalPolicy(env),
	})

	// ─── CloudFront ───────────────────────────────────────────────────────────

	// Extract domain from the Lambda Function URL (https://xxxx.lambda-url.region.on.aws/)
	lambdaDomain := awscdk.Fn_Select(
		jsii.Number(0),
		awscdk.Fn_Split(
			jsii.String("/"),
			awscdk.Fn_Select(jsii.Number(1), awscdk.Fn_Split(jsii.String("//"), fnUrl.Url(), jsii.Number(2))),
			jsii.Number(2),
		),
	)

	cdn := awscloudfront.NewDistribution(stack, jsii.String("CDN"), &awscloudfront.DistributionProps{
		Comment:           jsii.String("swim-signup-" + env + " — swim practice signup app"),
		PriceClass:        awscloudfront.PriceClass_PRICE_CLASS_100,
		HttpVersion:       awscloudfront.HttpVersion_HTTP2_AND_3,
		DefaultRootObject: jsii.String("index.html"),
		DefaultBehavior: &awscloudfront.BehaviorOptions{
			Origin:               awscloudfrontorigins.NewHttpOrigin(uiBucket.BucketWebsiteDomainName(), &awscloudfrontorigins.HttpOriginProps{
				ProtocolPolicy: awscloudfront.OriginProtocolPolicy_HTTP_ONLY,
			}),
			ViewerProtocolPolicy: awscloudfront.ViewerProtocolPolicy_REDIRECT_TO_HTTPS,
		},
		AdditionalBehaviors: &map[string]*awscloudfront.BehaviorOptions{
			"/api/*": {
				Origin: awscloudfrontorigins.NewHttpOrigin(lambdaDomain, &awscloudfrontorigins.HttpOriginProps{
					ProtocolPolicy: awscloudfront.OriginProtocolPolicy_HTTPS_ONLY,
				}),
				ViewerProtocolPolicy: awscloudfront.ViewerProtocolPolicy_HTTPS_ONLY,
				AllowedMethods:       awscloudfront.AllowedMethods_ALLOW_ALL(),
				CachePolicy:          awscloudfront.CachePolicy_CACHING_DISABLED(),
				OriginRequestPolicy:  awscloudfront.OriginRequestPolicy_ALL_VIEWER_EXCEPT_HOST_HEADER(),
			},
		},
		ErrorResponses: &[]*awscloudfront.ErrorResponse{
			{
				HttpStatus:         jsii.Number(404),
				ResponseHttpStatus: jsii.Number(200),
				ResponsePagePath:   jsii.String("/index.html"),
				Ttl:                awscdk.Duration_Seconds(jsii.Number(0)),
			},
		},
	})

	// ─── Outputs ─────────────────────────────────────────────────────────────

	awscdk.NewCfnOutput(stack, jsii.String("ApiUrl"), &awscdk.CfnOutputProps{
		Value:       fnUrl.Url(),
		Description: jsii.String("Lambda Function URL — set as VITE_API_URL when building the frontend"),
	})
	awscdk.NewCfnOutput(stack, jsii.String("UiBucketName"), &awscdk.CfnOutputProps{
		Value:       uiBucket.BucketName(),
		Description: jsii.String("S3 bucket — sync frontend build here"),
	})
	awscdk.NewCfnOutput(stack, jsii.String("UiUrl"), &awscdk.CfnOutputProps{
		Value:       uiBucket.BucketWebsiteUrl(),
		Description: jsii.String("Frontend URL"),
	})
	awscdk.NewCfnOutput(stack, jsii.String("CdnUrl"), &awscdk.CfnOutputProps{
		Value:       cdn.DistributionDomainName(),
		Description: jsii.String("CloudFront domain — access the app at https://{this}"),
	})

	return stack
}

// ensureLambdaZip creates a stub zip when the real binary hasn't been built yet
// so that CDK can synth and bootstrap without requiring a prior `make build`.
// The stub is replaced by the real binary when `make build` is run.
func ensureLambdaZip(path string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		panic("cannot create stub lambda.zip — check backend/ permissions: " + err.Error())
	}
	defer f.Close()
	w := zip.NewWriter(f)
	entry, err := w.Create("bootstrap")
	if err != nil {
		panic("cannot write stub lambda.zip: " + err.Error())
	}
	if _, err := entry.Write([]byte("#!/bin/sh\n")); err != nil {
		panic("cannot write stub lambda.zip: " + err.Error())
	}
	if err := w.Close(); err != nil {
		panic("cannot close stub lambda.zip: " + err.Error())
	}
}

func removalPolicy(env string) awscdk.RemovalPolicy {
	if env == "prod" {
		return awscdk.RemovalPolicy_RETAIN
	}
	return awscdk.RemovalPolicy_DESTROY
}

func main() {
	app := awscdk.NewApp(nil)

	envVal := app.Node().TryGetContext(jsii.String("env"))
	if envVal == nil {
		panic("environment required: cdk deploy -c env=dev  (or -c env=prod)")
	}
	env, ok := envVal.(string)
	if !ok {
		panic("env context value must be a string")
	}

	ensureLambdaZip("../backend/lambda.zip")

	NewSwimStack(app, "SwimStack-"+env, &SwimStackProps{
		StackProps: awscdk.StackProps{},
		Env:        env,
	})

	app.Synth(nil)
}
