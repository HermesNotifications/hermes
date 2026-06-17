# Hermes.ServerSdk.Api.TenantsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateTenant**](TenantsApi.md#createtenant) | **POST** /v1/tenants | Create a tenant |
| [**ListTenants**](TenantsApi.md#listtenants) | **GET** /v1/tenants | List tenants |

<a id="createtenant"></a>
# **CreateTenant**
> TenantItem CreateTenant (CreateTenantInputBody createTenantInputBody)

Create a tenant


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createTenantInputBody** | [**CreateTenantInputBody**](CreateTenantInputBody.md) |  |  |

### Return type

[**TenantItem**](TenantItem.md)

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

<a id="listtenants"></a>
# **ListTenants**
> List&lt;TenantItem&gt; ListTenants ()

List tenants


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;TenantItem&gt;**](TenantItem.md)

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

