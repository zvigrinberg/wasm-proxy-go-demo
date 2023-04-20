package main

import (
	"github.com/buger/jsonparser"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
	"os"
	"strconv"
	"strings"
)

var propagatedBodyValues RequestBodyDownstream = RequestBodyDownstream{}

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

// Structure for response body of the interceptor token endpoint

type InterceptorTokenResponse struct {
	access_token string
	expires_in   int
	token_type   string
	scope        string
}

// Structure to parse request body and propagate it to the request body of the request to be made to interceptor endpoint.

type RequestBodyDownstream struct {
	countryCode           string
	dataOwningCountryCode string
}

//go:generate go-json-ice --type=InterceptorRequestBody
type InterceptorRequestBody struct {
	countryCode           string
	dataOwningCountryCode string
	protectNullValues     bool
	preserveStringLength  bool
	manifestName          string
	restrictedText        string
	dataSet               string
	snapshotDate          string
	jobType               string
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
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &setBodyContext{}
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	data, err := proxywasm.GetPluginConfiguration()
	if err != nil {
		proxywasm.LogCriticalf("error reading plugin configuration: %v", err)
	} else {
		proxywasm.LogInfof("plugin configuration: %v", data)
	}

	return types.OnPluginStartStatusOK
}

type setBodyContext struct {
	// Embed the default root http context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultHttpContext
	modifyResponse         bool
	totalRequestBodySize   int
	totalResponseBodySize  int
	bufferOperation        string
	accessTokenInterceptor string
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {

	ctx.modifyResponse = true
	return types.ActionContinue
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpRequestBody(bodySize int, endOfStream bool) types.Action {

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
	//var clientBody RequestBodyDownstream
	//clientBody.UnmarshalJSON[]byte(originalBody))
	//json.Unmarshal([]byte(originalBody), &clientBody)

	propagatedBodyValues.countryCode, _ = jsonparser.GetString(originalBody, "countryCode")
	propagatedBodyValues.dataOwningCountryCode, _ = jsonparser.GetString(originalBody, "dataOwningCountryCode")

	//Get token from token endpoint
	clientId := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")
	grantType := "client_credentials"
	urlEncodedBody := "client_id=" + clientId + "&client_secret=" + clientSecret + "&grant_type=" + grantType
	proxywasm.LogInfof("urlEncodedBody= %s", urlEncodedBody)
	if _, err := proxywasm.DispatchHttpCall("interceptor_service", [][2]string{
		{":path", "/apigator/identity/v1/token"},
		{":method", "POST"},
		{":authority", "api.exate.co"},
		{"X-Api-Key", os.Getenv("API_KEY")},
		{"Content-Type", "application/x-www-form-urlencoded"}}, []byte(urlEncodedBody), nil,
		50000, ctx.dispatchCallbackToken); err != nil {
		proxywasm.LogCriticalf("dispatch httpcall to token endpoint failed: %v", err)
		return types.ActionContinue
	}

	return types.ActionPause
}

// Override types.DefaultHttpContext.
func (ctx *setBodyContext) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	// Remove Content-Length in order to prevent severs from crashing if we set different body.
	proxywasm.LogInfo("Removing Content-length header")
	if err := proxywasm.RemoveHttpResponseHeader("content-length"); err != nil {
		proxywasm.LogInfof("Failed removing the content-length header -  %v", err)
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

	interceptorRequestBody := InterceptorRequestBody{}
	interceptorRequestBody.dataSet = originalBodyJsonReady
	interceptorRequestBody.dataOwningCountryCode = propagatedBodyValues.dataOwningCountryCode
	interceptorRequestBody.countryCode = propagatedBodyValues.countryCode
	interceptorRequestBody.manifestName = os.Getenv("MANIFEST_NAME")
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

	//interceptorRequestBodyBytes, err := json.Marshal(interceptorRequestBody)
	interceptorRequestBodyBytes := strings.Builder{}
	interceptorRequestBodyBytes.WriteString("{")
	interceptorRequestBodyBytes.WriteString("\"countryCode\":" + " \"" + interceptorRequestBody.countryCode + "\", ")
	interceptorRequestBodyBytes.WriteString("\"dataOwningCountryCode\":" + " \"" + interceptorRequestBody.dataOwningCountryCode + "\", ")
	interceptorRequestBodyBytes.WriteString("\"protectNullValues\":" + " \"" + strings.ReplaceAll(strconv.FormatBool(interceptorRequestBody.protectNullValues), "\"", "") + "\", ")
	interceptorRequestBodyBytes.WriteString("\"preserveStringLength\":" + " \"" + strings.ReplaceAll(strconv.FormatBool(interceptorRequestBody.preserveStringLength), "\"", "") + "\", ")
	interceptorRequestBodyBytes.WriteString("\"restrictedText\":" + " \"" + interceptorRequestBody.restrictedText + "\", ")
	interceptorRequestBodyBytes.WriteString("\"dataSet\":" + " \"" + interceptorRequestBody.dataSet + "\", ")
	interceptorRequestBodyBytes.WriteString("\"manifestName\":" + " \"" + interceptorRequestBody.manifestName + "\", ")
	interceptorRequestBodyBytes.WriteString("\"snapshotDate\":" + " \"" + interceptorRequestBody.snapshotDate + "\", ")
	interceptorRequestBodyBytes.WriteString("\"jobType\":" + " \"" + interceptorRequestBody.jobType + "\"}")
	interceptorRequestBodyString := interceptorRequestBodyBytes.String()
	interceptorRequestBodyString = strings.ReplaceAll(interceptorRequestBodyString, "\"false\"", "false")
	interceptorRequestBodyString = strings.ReplaceAll(interceptorRequestBodyString, "\"true\"", "true")
	proxywasm.LogInfof("body to be sent to interceptor : %s ", interceptorRequestBodyString)

	//if err != nil {
	//	proxywasm.LogErrorf("failed to Marshal interceptor RequestBody struct into bytes :  %v", err)
	//}
	bearerToken := []string{"Bearer", ctx.accessTokenInterceptor}
	bearerTokenHeaderValue := strings.Join(bearerToken, " ")
	if _, err := proxywasm.DispatchHttpCall("interceptor_service", [][2]string{
		{":path", "/apigator/protect/v1/dataset"},
		{":method", "POST"},
		{":authority", "api.exate.co"},
		{"X-Api-Key", os.Getenv("API_KEY")},
		{"X-Resource-Token", bearerTokenHeaderValue},
		{"X-Data-Set-Type", "JSON"},
		{"Content-Type", "application/json"}}, []byte(interceptorRequestBodyString), nil,
		50000, ctx.dispatchCallbackInterceptor); err != nil {
		proxywasm.LogCriticalf("dispatch httpcall to interceptor endpoint failed: %v", err)
		return types.ActionContinue
	}
	return types.ActionPause

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

func (ctx *setBodyContext) dispatchCallbackToken(numHeaders, bodySize, numTrailers int) {
	// Clear access token field from old values
	ctx.accessTokenInterceptor = ""
	// This case, all the dispatched request was processed.
	// Adds a response header to the original response.
	//proxywasm.AddHttpResponseHeader("total-dispatched", strconv.Itoa(totalDispatchNum))
	proxywasm.LogInfof("body size - %d", bodySize)
	// And then contniue the original reponse.
	responseBodyToken, err := proxywasm.GetHttpCallResponseBody(0, bodySize)
	if err != nil {
		proxywasm.LogInfof("Failed to get response of token  - %v", err)
	}
	headers, _ := proxywasm.GetHttpResponseHeaders()

	proxywasm.LogInfo("token response HEADERS %s")
	for header := range headers {
		proxywasm.LogInfof("header - %s", header)
	}

	proxywasm.LogInfof("token response body %s", responseBodyToken)
	//json.Unmarshal(responseBodyToken, &tokenResponse)
	ctx.accessTokenInterceptor, _ = jsonparser.GetString(responseBodyToken, "access_token")

	proxywasm.ResumeHttpRequest()
	proxywasm.LogInfof("Request resumed after Obtained token for interceptor, token value= %s", ctx.accessTokenInterceptor)

}

func (ctx *setBodyContext) dispatchCallbackInterceptor(numHeaders, bodySize, numTrailers int) {
	responseBodyInterceptor, _ := proxywasm.GetHttpCallResponseBody(0, bodySize)
	headers, err := proxywasm.GetHttpCallResponseHeaders()
	if err != nil {
		proxywasm.LogCriticalf("error reading response headers from interceptor: %v", err)
	}

	proxywasm.ReplaceHttpResponseBody(responseBodyInterceptor)
	proxywasm.LogInfof("Response resumed after called interceptor, response body sent to downstream= %s Headers - %v ", string(responseBodyInterceptor), headers)
	proxywasm.ResumeHttpResponse()

}
