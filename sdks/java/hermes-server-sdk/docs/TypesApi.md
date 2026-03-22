# TypesApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createType**](TypesApi.md#createType) | **POST** /v1/types | Create a notification type |
| [**deleteType**](TypesApi.md#deleteType) | **DELETE** /v1/types/{id} | Delete a notification type |
| [**listTypes**](TypesApi.md#listTypes) | **GET** /v1/types | List notification types |
| [**updateType**](TypesApi.md#updateType) | **PUT** /v1/types/{id} | Update a notification type |


<a id="createType"></a>
# **createType**
> NotificationType createType(createTypeInputBody)

Create a notification type

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TypesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TypesApi apiInstance = new TypesApi(defaultClient);
    CreateTypeInputBody createTypeInputBody = new CreateTypeInputBody(); // CreateTypeInputBody | 
    try {
      NotificationType result = apiInstance.createType(createTypeInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TypesApi#createType");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **createTypeInputBody** | [**CreateTypeInputBody**](CreateTypeInputBody.md)|  | |

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

<a id="deleteType"></a>
# **deleteType**
> deleteType(id)

Delete a notification type

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TypesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TypesApi apiInstance = new TypesApi(defaultClient);
    String id = "id_example"; // String | Type ID
    try {
      apiInstance.deleteType(id);
    } catch (ApiException e) {
      System.err.println("Exception when calling TypesApi#deleteType");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **id** | **String**| Type ID | |

### Return type

null (empty response body)

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

<a id="listTypes"></a>
# **listTypes**
> List&lt;NotificationType&gt; listTypes()

List notification types

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TypesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TypesApi apiInstance = new TypesApi(defaultClient);
    try {
      List<NotificationType> result = apiInstance.listTypes();
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TypesApi#listTypes");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

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

<a id="updateType"></a>
# **updateType**
> NotificationType updateType(id, updateTypeInputBody)

Update a notification type

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TypesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TypesApi apiInstance = new TypesApi(defaultClient);
    String id = "id_example"; // String | Type ID
    UpdateTypeInputBody updateTypeInputBody = new UpdateTypeInputBody(); // UpdateTypeInputBody | 
    try {
      NotificationType result = apiInstance.updateType(id, updateTypeInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TypesApi#updateType");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **id** | **String**| Type ID | |
| **updateTypeInputBody** | [**UpdateTypeInputBody**](UpdateTypeInputBody.md)|  | |

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

