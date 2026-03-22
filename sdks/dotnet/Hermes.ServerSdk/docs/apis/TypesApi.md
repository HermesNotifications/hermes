# Hermes.ServerSdk.Api.TypesApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateType**](TypesApi.md#createtype) | **POST** /v1/types | Create a notification type |
| [**DeleteType**](TypesApi.md#deletetype) | **DELETE** /v1/types/{id} | Delete a notification type |
| [**ListTypes**](TypesApi.md#listtypes) | **GET** /v1/types | List notification types |
| [**UpdateType**](TypesApi.md#updatetype) | **PUT** /v1/types/{id} | Update a notification type |

<a id="createtype"></a>
# **CreateType**
> NotificationType CreateType (CreateTypeInputBody createTypeInputBody)

Create a notification type


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createTypeInputBody** | [**CreateTypeInputBody**](CreateTypeInputBody.md) |  |  |

### Return type

[**NotificationType**](NotificationType.md)

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

<a id="deletetype"></a>
# **DeleteType**
> void DeleteType (string id)

Delete a notification type


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Type ID |  |

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

<a id="listtypes"></a>
# **ListTypes**
> List&lt;NotificationType&gt; ListTypes ()

List notification types


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;NotificationType&gt;**](NotificationType.md)

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

<a id="updatetype"></a>
# **UpdateType**
> NotificationType UpdateType (string id, UpdateTypeInputBody updateTypeInputBody)

Update a notification type


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Type ID |  |
| **updateTypeInputBody** | [**UpdateTypeInputBody**](UpdateTypeInputBody.md) |  |  |

### Return type

[**NotificationType**](NotificationType.md)

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

