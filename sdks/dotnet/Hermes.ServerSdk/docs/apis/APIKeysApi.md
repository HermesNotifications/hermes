# Hermes.ServerSdk.Api.APIKeysApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateApiKey**](APIKeysApi.md#createapikey) | **POST** /v1/apikeys | Create a new API key |
| [**DeleteApiKey**](APIKeysApi.md#deleteapikey) | **DELETE** /v1/apikeys/{id} | Revoke an API key |
| [**ListApiKeys**](APIKeysApi.md#listapikeys) | **GET** /v1/apikeys | List all API keys |

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
 - **Accept**: application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **204** | No Content |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="listapikeys"></a>
# **ListApiKeys**
> List&lt;Item&gt; ListApiKeys ()

List all API keys


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;Item&gt;**](Item.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

