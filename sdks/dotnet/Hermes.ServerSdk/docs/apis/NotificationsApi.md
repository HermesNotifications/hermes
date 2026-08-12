# Hermes.ServerSdk.Api.NotificationsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|--------|--------------|-------------|
| [**GetNotification**](NotificationsApi.md#getnotification) | **GET** /v1/notifications/{id} | Get notification status and events |
| [**ListNotifications**](NotificationsApi.md#listnotifications) | **GET** /v1/notifications | List recent notifications |
| [**SendNotification**](NotificationsApi.md#sendnotification) | **POST** /v1/send | Send a notification |

<a id="getnotification"></a>
# **GetNotification**
> NotificationStatusOutputBody GetNotification (string id)

Get notification status and events


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **id** | **string** | Notification ID |  |

### Return type

[**NotificationStatusOutputBody**](NotificationStatusOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="listnotifications"></a>
# **ListNotifications**
> List&lt;NotificationItem&gt; ListNotifications (long limit = null)

List recent notifications


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **limit** | **long** | Max results (default 50) | [optional]  |

### Return type

[**List&lt;NotificationItem&gt;**](NotificationItem.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

<a id="sendnotification"></a>
# **SendNotification**
> SendOutputBody SendNotification (SendInputBody sendInputBody, string xIdempotencyKey = null)

Send a notification


### Parameters

| Name | Type | Description | Notes |
|------|------|-------------|-------|
| **sendInputBody** | [**SendInputBody**](SendInputBody.md) |  |  |
| **xIdempotencyKey** | **string** | Idempotency key for deduplication | [optional]  |

### Return type

[**SendOutputBody**](SendOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **202** | Accepted |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to Model list]](../../README.md#documentation-for-models) [[Back to README]](../../README.md)

