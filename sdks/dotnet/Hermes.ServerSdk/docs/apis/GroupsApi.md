# Hermes.ServerSdk.Api.GroupsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateGroup**](GroupsApi.md#creategroup) | **POST** /v1/groups | Create a notification group |
| [**ListGroups**](GroupsApi.md#listgroups) | **GET** /v1/groups | List notification groups |
| [**UpdateGroup**](GroupsApi.md#updategroup) | **PUT** /v1/groups/{id} | Update a notification group |

<a id="creategroup"></a>
# **CreateGroup**
> NotificationGroup CreateGroup (CreateGroupInputBody createGroupInputBody)

Create a notification group


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createGroupInputBody** | [**CreateGroupInputBody**](CreateGroupInputBody.md) |  |  |

### Return type

[**NotificationGroup**](NotificationGroup.md)

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

<a id="listgroups"></a>
# **ListGroups**
> List&lt;NotificationGroup&gt; ListGroups ()

List notification groups


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;NotificationGroup&gt;**](NotificationGroup.md)

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

<a id="updategroup"></a>
# **UpdateGroup**
> NotificationGroup UpdateGroup (string id, UpdateGroupInputBody updateGroupInputBody)

Update a notification group


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Group ID |  |
| **updateGroupInputBody** | [**UpdateGroupInputBody**](UpdateGroupInputBody.md) |  |  |

### Return type

[**NotificationGroup**](NotificationGroup.md)

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

