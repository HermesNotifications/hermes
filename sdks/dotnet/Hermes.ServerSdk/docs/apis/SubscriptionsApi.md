# Hermes.ServerSdk.Api.SubscriptionsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**CreateSubscription**](SubscriptionsApi.md#createsubscription) | **POST** /v1/subscriptions/categories/{category_id}/subscriptions | Create a subscription |
| [**CreateSubscriptionCategory**](SubscriptionsApi.md#createsubscriptioncategory) | **POST** /v1/subscriptions/categories | Create a subscription category |
| [**DeleteSubscription**](SubscriptionsApi.md#deletesubscription) | **DELETE** /v1/subscriptions/{id} | Delete a subscription |
| [**DeleteSubscriptionCategory**](SubscriptionsApi.md#deletesubscriptioncategory) | **DELETE** /v1/subscriptions/categories/{id} | Delete a subscription category |
| [**ListSubscriptionCategories**](SubscriptionsApi.md#listsubscriptioncategories) | **GET** /v1/subscriptions/categories | List subscription categories |
| [**ListSubscriptions**](SubscriptionsApi.md#listsubscriptions) | **GET** /v1/subscriptions/categories/{category_id}/subscriptions | List subscriptions in a category |
| [**UpdateSubscription**](SubscriptionsApi.md#updatesubscription) | **PUT** /v1/subscriptions/{id} | Update a subscription |
| [**UpdateSubscriptionCategory**](SubscriptionsApi.md#updatesubscriptioncategory) | **PUT** /v1/subscriptions/categories/{id} | Update a subscription category |

<a id="createsubscription"></a>
# **CreateSubscription**
> Subscription CreateSubscription (string categoryId, CreateSubscriptionInputBody createSubscriptionInputBody)

Create a subscription


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **categoryId** | **string** | Category ID |  |
| **createSubscriptionInputBody** | [**CreateSubscriptionInputBody**](CreateSubscriptionInputBody.md) |  |  |

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
| **201** | Created |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="createsubscriptioncategory"></a>
# **CreateSubscriptionCategory**
> SubscriptionCategory CreateSubscriptionCategory (CreateCategoryInputBody createCategoryInputBody)

Create a subscription category


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **createCategoryInputBody** | [**CreateCategoryInputBody**](CreateCategoryInputBody.md) |  |  |

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
| **201** | Created |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="deletesubscription"></a>
# **DeleteSubscription**
> void DeleteSubscription (string id)

Delete a subscription


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Subscription ID |  |

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

<a id="deletesubscriptioncategory"></a>
# **DeleteSubscriptionCategory**
> void DeleteSubscriptionCategory (string id)

Delete a subscription category


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Category ID |  |

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

<a id="listsubscriptioncategories"></a>
# **ListSubscriptionCategories**
> List&lt;SubscriptionCategory&gt; ListSubscriptionCategories ()

List subscription categories


### Parameters
This endpoint does not need any parameter.
### Return type

[**List&lt;SubscriptionCategory&gt;**](SubscriptionCategory.md)

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

<a id="listsubscriptions"></a>
# **ListSubscriptions**
> List&lt;Subscription&gt; ListSubscriptions (string categoryId)

List subscriptions in a category


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **categoryId** | **string** | Category ID |  |

### Return type

[**List&lt;Subscription&gt;**](Subscription.md)

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

<a id="updatesubscription"></a>
# **UpdateSubscription**
> Subscription UpdateSubscription (string id, UpdateSubscriptionInputBody updateSubscriptionInputBody)

Update a subscription


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Subscription ID |  |
| **updateSubscriptionInputBody** | [**UpdateSubscriptionInputBody**](UpdateSubscriptionInputBody.md) |  |  |

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
| **200** | OK |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="updatesubscriptioncategory"></a>
# **UpdateSubscriptionCategory**
> SubscriptionCategory UpdateSubscriptionCategory (string id, UpdateCategoryInputBody updateCategoryInputBody)

Update a subscription category


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Category ID |  |
| **updateCategoryInputBody** | [**UpdateCategoryInputBody**](UpdateCategoryInputBody.md) |  |  |

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
| **200** | OK |  -  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

