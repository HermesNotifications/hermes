# Hermes.ServerSdk.Api.OrganizationsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateOrganization**](OrganizationsApi.md#createorganization) | **POST** /v1/organizations | Create an organization |
| [**ListOrganizations**](OrganizationsApi.md#listorganizations) | **GET** /v1/organizations | List organizations |

<a id="createorganization"></a>
# **CreateOrganization**
> OrganizationItem CreateOrganization (CreateOrganizationInputBody createOrganizationInputBody)

Create an organization


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createOrganizationInputBody** | [**CreateOrganizationInputBody**](CreateOrganizationInputBody.md) |  |  |

### Return type

[**OrganizationItem**](OrganizationItem.md)

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

<a id="listorganizations"></a>
# **ListOrganizations**
> List&lt;OrganizationItem&gt; ListOrganizations ()

List organizations


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;OrganizationItem&gt;**](OrganizationItem.md)

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

