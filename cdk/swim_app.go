package main

import (
	"archive/zip"
	"os"

	"github.com/aws/aws-cdk-go/awscdk/v2"
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
		Cors: &awslambda.FunctionUrlCorsOptions{
			AllowedOrigins: &[]*string{jsii.String("*")},
			AllowedMethods: &[]awslambda.HttpMethod{
				awslambda.HttpMethod_GET,
				awslambda.HttpMethod_POST,
				awslambda.HttpMethod_DELETE,
			},
			AllowedHeaders: &[]*string{
				jsii.String("Content-Type"),
				jsii.String("X-Swimmer-Email"),
			},
			MaxAge: awscdk.Duration_Seconds(jsii.Number(300)),
		},
	})

	// ─── S3 (UI) ─────────────────────────────────────────────────────────────

	uiBucket := awss3.NewBucket(stack, jsii.String("UiBucket"), &awss3.BucketProps{
		BucketName:           jsii.String("swim-signup-ui" + sfx),
		WebsiteIndexDocument: jsii.String("index.html"),
		WebsiteErrorDocument: jsii.String("index.html"),
		PublicReadAccess:     jsii.Bool(true),
		BlockPublicAccess:    awss3.BlockPublicAccess_BLOCK_ACLS(),
		AutoDeleteObjects:    jsii.Bool(env != "prod"),
		RemovalPolicy:        removalPolicy(env),
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
