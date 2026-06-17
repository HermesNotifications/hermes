# hermes_server_sdk.TenantsApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_tenant**](TenantsApi.md#create_tenant) | **POST** /v1/tenants | Create a tenant
[**list_tenants**](TenantsApi.md#list_tenants) | **GET** /v1/tenants | List tenants


# **create_tenant**
> TenantItem create_tenant(create_tenant_input_body)

Create a tenant

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_tenant_input_body import CreateTenantInputBody
from hermes_server_sdk.models.tenant_item import TenantItem
from hermes_server_sdk.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = hermes_server_sdk.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with hermes_server_sdk.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = hermes_server_sdk.TenantsApi(api_client)
    create_tenant_input_body = hermes_server_sdk.CreateTenantInputBody() # CreateTenantInputBody | 

    try:
        # Create a tenant
        api_response = api_instance.create_tenant(create_tenant_input_body)
        print("The response of TenantsApi->create_tenant:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling TenantsApi->create_tenant: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_tenant_input_body** | [**CreateTenantInputBody**](CreateTenantInputBody.md)|  | 

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
**201** | Created |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_tenants**
> List[TenantItem] list_tenants()

List tenants

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.tenant_item import TenantItem
from hermes_server_sdk.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = hermes_server_sdk.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with hermes_server_sdk.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = hermes_server_sdk.TenantsApi(api_client)

    try:
        # List tenants
        api_response = api_instance.list_tenants()
        print("The response of TenantsApi->list_tenants:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling TenantsApi->list_tenants: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[TenantItem]**](TenantItem.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

