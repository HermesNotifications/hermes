# TemplatesApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createTemplate**](TemplatesApi.md#createTemplate) | **POST** /v1/templates | Create a notification template |
| [**deleteTemplate**](TemplatesApi.md#deleteTemplate) | **DELETE** /v1/templates/{id} | Delete a notification template |
| [**listTemplates**](TemplatesApi.md#listTemplates) | **GET** /v1/templates | List notification templates |
| [**updateTemplate**](TemplatesApi.md#updateTemplate) | **PUT** /v1/templates/{id} | Update a notification template |


<a id="createTemplate"></a>
# **createTemplate**
> NotificationTemplate createTemplate(createTemplateInputBody)

Create a notification template

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TemplatesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TemplatesApi apiInstance = new TemplatesApi(defaultClient);
    CreateTemplateInputBody createTemplateInputBody = new CreateTemplateInputBody(); // CreateTemplateInputBody | 
    try {
      NotificationTemplate result = apiInstance.createTemplate(createTemplateInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TemplatesApi#createTemplate");
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
| **createTemplateInputBody** | [**CreateTemplateInputBody**](CreateTemplateInputBody.md)|  | |

### Return type

[**NotificationTemplate**](NotificationTemplate.md)

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

<a id="deleteTemplate"></a>
# **deleteTemplate**
> deleteTemplate(id)

Delete a notification template

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TemplatesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TemplatesApi apiInstance = new TemplatesApi(defaultClient);
    String id = "id_example"; // String | Template ID
    try {
      apiInstance.deleteTemplate(id);
    } catch (ApiException e) {
      System.err.println("Exception when calling TemplatesApi#deleteTemplate");
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
| **id** | **String**| Template ID | |

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

<a id="listTemplates"></a>
# **listTemplates**
> List&lt;NotificationTemplate&gt; listTemplates()

List notification templates

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TemplatesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TemplatesApi apiInstance = new TemplatesApi(defaultClient);
    try {
      List<NotificationTemplate> result = apiInstance.listTemplates();
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TemplatesApi#listTemplates");
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

[**List&lt;NotificationTemplate&gt;**](NotificationTemplate.md)

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

<a id="updateTemplate"></a>
# **updateTemplate**
> NotificationTemplate updateTemplate(id, updateTemplateInputBody)

Update a notification template

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.TemplatesApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    TemplatesApi apiInstance = new TemplatesApi(defaultClient);
    String id = "id_example"; // String | Template ID
    UpdateTemplateInputBody updateTemplateInputBody = new UpdateTemplateInputBody(); // UpdateTemplateInputBody | 
    try {
      NotificationTemplate result = apiInstance.updateTemplate(id, updateTemplateInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling TemplatesApi#updateTemplate");
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
| **id** | **String**| Template ID | |
| **updateTemplateInputBody** | [**UpdateTemplateInputBody**](UpdateTemplateInputBody.md)|  | |

### Return type

[**NotificationTemplate**](NotificationTemplate.md)

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

