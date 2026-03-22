# Hermes.ServerSdk.Api.AuthApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**ExchangeToken**](AuthApi.md#exchangetoken) | **POST** /v1/auth/token | Exchange credentials for a user JWT token |

<a id="exchangetoken"></a>
# **ExchangeToken**
> TokenOutputBody ExchangeToken (TokenInputBody tokenInputBody)

Exchange credentials for a user JWT token


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **tokenInputBody** | [**TokenInputBody**](TokenInputBody.md) |  |  |

### Return type

[**TokenOutputBody**](TokenOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

