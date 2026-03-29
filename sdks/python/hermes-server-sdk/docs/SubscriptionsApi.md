# hermes_server_sdk.SubscriptionsApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_subscription**](SubscriptionsApi.md#create_subscription) | **POST** /v1/subscriptions/categories/{category_id}/subscriptions | Create a subscription
[**create_subscription_category**](SubscriptionsApi.md#create_subscription_category) | **POST** /v1/subscriptions/categories | Create a subscription category
[**delete_subscription**](SubscriptionsApi.md#delete_subscription) | **DELETE** /v1/subscriptions/{id} | Delete a subscription
[**delete_subscription_category**](SubscriptionsApi.md#delete_subscription_category) | **DELETE** /v1/subscriptions/categories/{id} | Delete a subscription category
[**list_subscription_categories**](SubscriptionsApi.md#list_subscription_categories) | **GET** /v1/subscriptions/categories | List subscription categories
[**list_subscriptions**](SubscriptionsApi.md#list_subscriptions) | **GET** /v1/subscriptions/categories/{category_id}/subscriptions | List subscriptions in a category
[**update_subscription**](SubscriptionsApi.md#update_subscription) | **PUT** /v1/subscriptions/{id} | Update a subscription
[**update_subscription_category**](SubscriptionsApi.md#update_subscription_category) | **PUT** /v1/subscriptions/categories/{id} | Update a subscription category


# **create_subscription**
> Subscription create_subscription(category_id, create_subscription_input_body)

Create a subscription

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_subscription_input_body import CreateSubscriptionInputBody
from hermes_server_sdk.models.subscription import Subscription
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    category_id = 'category_id_example' # str | Category ID
    create_subscription_input_body = hermes_server_sdk.CreateSubscriptionInputBody() # CreateSubscriptionInputBody | 

    try:
        # Create a subscription
        api_response = api_instance.create_subscription(category_id, create_subscription_input_body)
        print("The response of SubscriptionsApi->create_subscription:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->create_subscription: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **category_id** | **str**| Category ID | 
 **create_subscription_input_body** | [**CreateSubscriptionInputBody**](CreateSubscriptionInputBody.md)|  | 

### Return type

[**Subscription**](Subscription.md)

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

# **create_subscription_category**
> SubscriptionCategory create_subscription_category(create_category_input_body)

Create a subscription category

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_category_input_body import CreateCategoryInputBody
from hermes_server_sdk.models.subscription_category import SubscriptionCategory
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    create_category_input_body = hermes_server_sdk.CreateCategoryInputBody() # CreateCategoryInputBody | 

    try:
        # Create a subscription category
        api_response = api_instance.create_subscription_category(create_category_input_body)
        print("The response of SubscriptionsApi->create_subscription_category:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->create_subscription_category: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_category_input_body** | [**CreateCategoryInputBody**](CreateCategoryInputBody.md)|  | 

### Return type

[**SubscriptionCategory**](SubscriptionCategory.md)

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

# **delete_subscription**
> delete_subscription(id)

Delete a subscription

### Example


```python
import hermes_server_sdk
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    id = 'id_example' # str | Subscription ID

    try:
        # Delete a subscription
        api_instance.delete_subscription(id)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->delete_subscription: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Subscription ID | 

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
**204** | No Content |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_subscription_category**
> delete_subscription_category(id)

Delete a subscription category

### Example


```python
import hermes_server_sdk
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    id = 'id_example' # str | Category ID

    try:
        # Delete a subscription category
        api_instance.delete_subscription_category(id)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->delete_subscription_category: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Category ID | 

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
**204** | No Content |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_subscription_categories**
> List[SubscriptionCategory] list_subscription_categories()

List subscription categories

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.subscription_category import SubscriptionCategory
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)

    try:
        # List subscription categories
        api_response = api_instance.list_subscription_categories()
        print("The response of SubscriptionsApi->list_subscription_categories:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->list_subscription_categories: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[SubscriptionCategory]**](SubscriptionCategory.md)

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

# **list_subscriptions**
> List[Subscription] list_subscriptions(category_id)

List subscriptions in a category

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.subscription import Subscription
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    category_id = 'category_id_example' # str | Category ID

    try:
        # List subscriptions in a category
        api_response = api_instance.list_subscriptions(category_id)
        print("The response of SubscriptionsApi->list_subscriptions:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->list_subscriptions: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **category_id** | **str**| Category ID | 

### Return type

[**List[Subscription]**](Subscription.md)

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

# **update_subscription**
> Subscription update_subscription(id, update_subscription_input_body)

Update a subscription

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.subscription import Subscription
from hermes_server_sdk.models.update_subscription_input_body import UpdateSubscriptionInputBody
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    id = 'id_example' # str | Subscription ID
    update_subscription_input_body = hermes_server_sdk.UpdateSubscriptionInputBody() # UpdateSubscriptionInputBody | 

    try:
        # Update a subscription
        api_response = api_instance.update_subscription(id, update_subscription_input_body)
        print("The response of SubscriptionsApi->update_subscription:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->update_subscription: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Subscription ID | 
 **update_subscription_input_body** | [**UpdateSubscriptionInputBody**](UpdateSubscriptionInputBody.md)|  | 

### Return type

[**Subscription**](Subscription.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_subscription_category**
> SubscriptionCategory update_subscription_category(id, update_category_input_body)

Update a subscription category

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.subscription_category import SubscriptionCategory
from hermes_server_sdk.models.update_category_input_body import UpdateCategoryInputBody
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
    api_instance = hermes_server_sdk.SubscriptionsApi(api_client)
    id = 'id_example' # str | Category ID
    update_category_input_body = hermes_server_sdk.UpdateCategoryInputBody() # UpdateCategoryInputBody | 

    try:
        # Update a subscription category
        api_response = api_instance.update_subscription_category(id, update_category_input_body)
        print("The response of SubscriptionsApi->update_subscription_category:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling SubscriptionsApi->update_subscription_category: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Category ID | 
 **update_category_input_body** | [**UpdateCategoryInputBody**](UpdateCategoryInputBody.md)|  | 

### Return type

[**SubscriptionCategory**](SubscriptionCategory.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

