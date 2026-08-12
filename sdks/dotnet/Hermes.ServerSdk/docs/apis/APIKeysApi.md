# Hermes.ServerSdk.Api.APIKeysApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateApiKey**](APIKeysApi.md#createapikey) | **POST** /v1/apikeys | Create a new API key |
| [**DeleteApiKey**](APIKeysApi.md#deleteapikey) | **DELETE** /v1/apikeys/{id} | Revoke an API key |
| [**ListApiKeys**](APIKeysApi.md#listapikeys) | **GET** /v1/apikeys | List all API keys |
| [**SetApiKeyRateLimit**](APIKeysApi.md#setapikeyratelimit) | **PUT** /v1/apikeys/{id}/rate-limit | Set or clear a key&#39;s rate limit |

<a id="createapikey"></a>
# **CreateApiKey**
> ApiKeyCreatedOutputBody CreateApiKey (CreateAPIKeyInputBody createAPIKeyInputBody)

Create a new API key


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createAPIKeyInputBody** | [**CreateAPIKeyInputBody**](CreateAPIKeyInputBody.md) |  |  |

### Return type

[**ApiKeyCreatedOutputBody**](ApiKeyCreatedOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **201** | Created |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="deleteapikey"></a>
# **DeleteApiKey**
> void DeleteApiKey (string id)

Revoke an API key


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | API key ID |  |

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **204** | No Content |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="listapikeys"></a>
# **ListApiKeys**
> List&lt;ApiKeyView&gt; ListApiKeys ()

List all API keys


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;ApiKeyView&gt;**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="setapikeyratelimit"></a>
# **SetApiKeyRateLimit**
> ApiKeyView SetApiKeyRateLimit (string id, SetAPIKeyRateLimitInputBody setAPIKeyRateLimitInputBody)

Set or clear a key's rate limit

Replaces this key's rate limit. Omitted fields reset to the service default, so an empty body clears the override entirely. This endpoint invalidates the key's cache entry, so the new limit applies to the next bucket created for it — but a caller that is continuously active keeps its current bucket, and therefore its old limit, until it goes idle. Do not rely on this to throttle a caller mid-flood.


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | API key ID |  |
| **setAPIKeyRateLimitInputBody** | [**SetAPIKeyRateLimitInputBody**](SetAPIKeyRateLimitInputBody.md) |  |  |

### Return type

[**ApiKeyView**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

