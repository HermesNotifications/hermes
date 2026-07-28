# hermes_server_sdk.OrganizationsApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_organization**](OrganizationsApi.md#create_organization) | **POST** /v1/organizations | Create an organization
[**list_organizations**](OrganizationsApi.md#list_organizations) | **GET** /v1/organizations | List organizations


# **create_organization**
> OrganizationItem create_organization(create_organization_input_body)

Create an organization

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_organization_input_body import CreateOrganizationInputBody
from hermes_server_sdk.models.organization_item import OrganizationItem
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
    api_instance = hermes_server_sdk.OrganizationsApi(api_client)
    create_organization_input_body = hermes_server_sdk.CreateOrganizationInputBody() # CreateOrganizationInputBody | 

    try:
        # Create an organization
        api_response = api_instance.create_organization(create_organization_input_body)
        print("The response of OrganizationsApi->create_organization:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OrganizationsApi->create_organization: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_organization_input_body** | [**CreateOrganizationInputBody**](CreateOrganizationInputBody.md)|  | 

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
**201** | Created |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_organizations**
> List[OrganizationItem] list_organizations()

List organizations

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.organization_item import OrganizationItem
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
    api_instance = hermes_server_sdk.OrganizationsApi(api_client)

    try:
        # List organizations
        api_response = api_instance.list_organizations()
        print("The response of OrganizationsApi->list_organizations:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OrganizationsApi->list_organizations: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[OrganizationItem]**](OrganizationItem.md)

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

