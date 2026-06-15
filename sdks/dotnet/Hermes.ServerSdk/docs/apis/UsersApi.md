# Hermes.ServerSdk.Api.UsersApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**ListUsers**](UsersApi.md#listusers) | **GET** /v1/users | List users |

<a id="listusers"></a>
# **ListUsers**
> List&lt;UserItem&gt; ListUsers (string tenantId = null)

List users


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **tenantId** | **string** | Filter by tenant ID | [optional]  |

### Return type

[**List&lt;UserItem&gt;**](UserItem.md)

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

