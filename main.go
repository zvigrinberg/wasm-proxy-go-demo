package main

import (
	"bytes"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"net/http"
	"net/url"
)
var propagatedBodyValues RequestBodyDownstream = RequestBodyDownstream{}
const (
	bufferOperationAppend  = "append"
	bufferOperationPrepend = "prepend"
	bufferOperationReplace = "replace"
)

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

// Structure for response body of the interceptor token endpoint
type InterceptorTokenResponse struct {
	access_token string
	expires_in int
	token_type string
	scope string

}
// Structure to parse request body and propagate it to the request body of the request to be made to interceptor endpoint.
type RequestBodyDownstream struct {
	countryCode string
	dataOwningCountryCode string
}

//
type InterceptorRequestBody struct {
	countryCode string
	dataOwningCountryCode string
	protectNullValues bool
	preserveStringLength bool
	manifestName string
	restrictedText string
	dataSet string
	snapshotDate string
	jobType string

}

type vmContext struct {
	// Embed the default VM context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultVMContext
}

// Override types.DefaultVMContext.
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{}
}

type pluginContext struct {
	// Embed the default plugin context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultPluginContext
	shouldEchoBody bool
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	if ctx.shouldEchoBody {
		return &echoBodyContext{}
	}
	return &setBodyContext{}
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	data, err := proxywasm.GetPluginConfiguration()
	if err != nil {
		proxywasm.LogCriticalf("error reading plugin configuration: %v", err)
	}
	ctx.shouldEchoBody = string(data) == "echo"
	return types.OnPluginStartStatusOK
}

type setBodyContext struct {
	// Embed the default root http context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultHttpContext
	modifyResponse        bool
	totalRequestBodySize  int
	totalResponseBodySize int
	bufferOperation       string
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	mode, err := proxywasm.GetHttpRequestHeader("buffer-replace-at")
	if mode == "response" {
		ctx.modifyResponse = true
	}

	if _, err := proxywasm.GetHttpRequestHeader("content-length"); err != nil {
		if err := proxywasm.SendHttpResponse(400, nil, []byte("content must be provided"), -1); err != nil {
			panic(err)
		}
		return types.ActionPause
	}

	// Remove Content-Length in order to prevent severs from crashing if we set different body from downstream.
	if err := proxywasm.RemoveHttpRequestHeader("content-length"); err != nil {
		panic(err)
	}

	// Get "Buffer-Operation" header value.
	op, err := proxywasm.GetHttpRequestHeader("buffer-operation")
	if err != nil || (op != bufferOperationAppend &&
		op != bufferOperationPrepend &&
		op != bufferOperationReplace) {
		// Fallback to replace
		op = bufferOperationReplace
	}
	ctx.bufferOperation = op
	return types.ActionContinue
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpRequestBody(bodySize int, endOfStream bool) types.Action {
	if ctx.modifyResponse {
		return types.ActionContinue
	}

	ctx.totalRequestBodySize += bodySize
	if !endOfStream {
		// Wait until we see the entire body to replace.
		return types.ActionPause
	}

	originalBody, err := proxywasm.GetHttpRequestBody(0, ctx.totalRequestBodySize)
	if err != nil {
		proxywasm.LogErrorf("failed to get request body: %v", err)
		return types.ActionContinue
	}
	proxywasm.LogInfof("original request body: %s", string(originalBody))
	var clientBody RequestBodyDownstream
    json.Unmarshal([]byte(originalBody),&clientBody)

	//switch ctx.bufferOperation {
	//case bufferOperationAppend:
	//	err = proxywasm.AppendHttpRequestBody([]byte(`[this is appended body]`))
	//case bufferOperationPrepend:
	//	err = proxywasm.PrependHttpRequestBody([]byte(`[this is prepended body]`))
	//case bufferOperationReplace:
	//	err = proxywasm.ReplaceHttpRequestBody([]byte(`[this is replaced body]`))
	//}
	//if err != nil {
	//	proxywasm.LogErrorf("failed to %s request body: %v", ctx.bufferOperation, err)
	//	return types.ActionContinue
	//}
	propagatedBodyValues.countryCode = clientBody.countryCode
	propagatedBodyValues.dataOwningCountryCode = clientBody.dataOwningCountryCode
	return types.ActionContinue
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	if !ctx.modifyResponse {
		return types.ActionContinue
	}

	// Remove Content-Length in order to prevent severs from crashing if we set different body.
	if err := proxywasm.RemoveHttpResponseHeader("content-length"); err != nil {
		panic(err)
	}

	return types.ActionContinue
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpResponseBody(bodySize int, endOfStream bool) types.Action {
	if !ctx.modifyResponse {
		return types.ActionContinue
	}

	ctx.totalResponseBodySize += bodySize
	if !endOfStream {
		// Wait until we see the entire body to replace.
		return types.ActionPause
	}

	originalBody, err := proxywasm.GetHttpResponseBody(0, ctx.totalResponseBodySize)
	if err != nil {
		proxywasm.LogErrorf("failed to get response body: %v", err)
		return types.ActionContinue
	}
	proxywasm.LogInfof("original response body: %s", string(originalBody))

	// replace all double quotes with single quotes for interceptor.
	originalBodyJson := string(originalBody)
	originalBodyJsonReady := strings.ReplaceAll(originalBodyJson, "\"", "'")


	//Get token from token endpoint

	interceptorUrl := "https://api.exate.co"
	tokenResource := "/apigator/identity/v1/token"
	data :=url.Values{}
	data.Set("client_id",os.Getenv("CLIENT_ID"))
	data.Set("client_secret", os.Getenv("CLIENT_SECRET"))
	data.Set("grant_type", "client_credentials")

    parsedTokenUrl, _ := url.ParseRequestURI(interceptorUrl)
	parsedTokenUrl.Path = tokenResource
	parsedTokenUrlString := parsedTokenUrl.String()

	client := &http.Client{}
	tokenRequest, _ :=  http.NewRequest(http.MethodPost, parsedTokenUrlString, strings.NewReader(data.Encode()))
	tokenRequest.Header.Add("X-Api-Key", os.Getenv("API_KEY"))
	tokenRequest.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	response, _ := client.Do(tokenRequest)
	theResponse, err := io.ReadAll(response.Body)
	if err != nil {
		proxywasm.LogErrorf("failed to read body of token response payload :  %v",  err)
	}
	response.Body.Close()
	tokenResponse := InterceptorTokenResponse{}
	json.Unmarshal(theResponse,&tokenResponse)
	theAccessToken := tokenResponse.access_token
	interceptorRequestBody := InterceptorRequestBody{}
	interceptorRequestBody.dataSet = originalBodyJsonReady
	interceptorRequestBody.dataOwningCountryCode = propagatedBodyValues.dataOwningCountryCode
	interceptorRequestBody.countryCode = propagatedBodyValues.countryCode
	interceptorRequestBody.jobType = os.Getenv("JOB_TYPE")
	interceptorRequestBody.snapshotDate = "2023-03-20T00:00:00Z"
	interceptorRequestBody.restrictedText = os.Getenv("RESTRICTED_TEXT")
	protectNullValues, err := strconv.ParseBool(os.Getenv("PROTECT_NULL_VALUES"))
	if err != nil {
		protectNullValues = false
	}

	interceptorRequestBody.protectNullValues = protectNullValues

	preserveStringLength, err := strconv.ParseBool(os.Getenv("PRESERVE_STRING_LENGTH"))
	if err != nil {
		preserveStringLength = false
	}

	interceptorRequestBody.preserveStringLength = preserveStringLength

	InterceptorResource := "/apigator/protect/v1/dataset"
	parsedInterceptorUrl, _ := url.ParseRequestURI(interceptorUrl)
	parsedInterceptorUrl.Path = InterceptorResource
	parsedInterceptorUrlString := parsedInterceptorUrl.String()
	interceptorRequestBodyBytes, err := json.Marshal(interceptorRequestBody)
	if err != nil {
		proxywasm.LogErrorf("failed to Marshal interceptor RequestBody struct into bytes :  %v",  err)
	}
	InterceptorRequest, _ :=  http.NewRequest(http.MethodPost, parsedInterceptorUrlString, bytes.NewBuffer(interceptorRequestBodyBytes))
	InterceptorRequest.Header.Add("Content-Type", "application/json")
	InterceptorRequest.Header.Add("X-Data-Set-Type", "JSON")
	InterceptorRequest.Header.Add("X-Api-Key", os.Getenv("API_KEY"))
	bearerToken := []string {"Bearer", theAccessToken}

	InterceptorRequest.Header.Add("X-Resource-Token", strings.Join(bearerToken," "))
	responseInterceptor, _ := client.Do(InterceptorRequest)
	theInterceptorResponse, err := io.ReadAll(responseInterceptor.Body)
	if err != nil {
		proxywasm.LogErrorf("failed to read body of token response payload :  %v",  err)
	}
	responseInterceptor.Body.Close()
	err = proxywasm.ReplaceHttpResponseBody(theInterceptorResponse)

	if err != nil {
		proxywasm.LogErrorf("failed to %s response body: %v",  err)
		return types.ActionContinue
	}
	return types.ActionContinue
}

type echoBodyContext struct {
	// mbed the default plugin context
	// so that you don't need to reimplement all the methods by yourself.
	types.DefaultHttpContext
	totalRequestBodySize int
}

//// Override types.DefaultHttpContext.
//func (ctx *echoBodyContext) OnHttpRequestBody(bodySize int, endOfStream bool) types.Action {
//	ctx.totalRequestBodySize += bodySize
//	if !endOfStream {
//		// Wait until we see the entire body to replace.
//		return types.ActionPause
//	}
//
//	// Send the request body as the response body.
//	body, _ := proxywasm.GetHttpRequestBody(0, ctx.totalRequestBodySize)
//	if err := proxywasm.SendHttpResponse(200, nil, body, -1); err != nil {
//		panic(err)
//	}
//	return types.ActionPause
//}
