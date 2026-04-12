// ObsyncianConfig matches the Go desktop app format
export interface Credentials {
  key: string;
  secret: string;
}

export interface ObsyncianConfig {
  id: string; // UUID
  local: string; // vault path (overridden on Android)
  cloud: string; // S3 bucket name
  provider: string; // e.g., "AWS"
  snsTopicArn: string; // optional for SQS/SNS events
  knowledgeBaseId: string; // Bedrock KB ID (not used in sync)
  dataSourceId: string; // Bedrock data source ID (not used in sync)
  region: string; // AWS region
  credentials: Credentials;
}

export function parseConfig(jsonStr: string): ObsyncianConfig {
  // Remove UTF-8 BOM if present
  const cleaned = jsonStr.replace(/^\ufeff/, '');
  const data = JSON.parse(cleaned);
  return {
    id: data.id || '',
    local: data.local || '',
    cloud: data.cloud || '',
    provider: data.provider || 'AWS',
    snsTopicArn: data.snsTopicArn || '',
    knowledgeBaseId: data.knowledgeBaseId || '',
    dataSourceId: data.dataSourceId || '',
    region: data.region || 'ap-southeast-2',
    credentials: {
      key: data.credentials?.key || '',
      secret: data.credentials?.secret || '',
    },
  };
}

export function isValidConfig(config: ObsyncianConfig): boolean {
  return (
    config.id.trim() !== '' &&
    config.local.trim() !== '' &&
    config.cloud.trim() !== '' &&
    config.credentials.key.trim() !== '' &&
    config.credentials.secret.trim() !== ''
  );
}
